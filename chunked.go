package main

// Chunked uploads, for big files and for sending one file over several
// connections at once. They map onto an S3 multipart upload:
//
//	POST /chunked/start?filename=NAME           -> {"key": KEY, "uploadId": ID}
//	PUT  /chunked/part?key=KEY&uploadId=ID&n=N  (body: chunk N, from 1) -> {"n": N, "etag": ETAG}
//	POST /chunked/finish?key=KEY&uploadId=ID    (body: JSON [{"n": N, "etag": ETAG}, ...]) -> URL
//	POST /chunked/abort?key=KEY&uploadId=ID
//
// Parts can go up in any order and in parallel. Every part but the last must
// be at least 5 MB (an S3 rule). Bucky never sees the whole file, so the key
// starts with a random ID instead of the file's SHA-256. The URL comes back
// the same way as from a normal upload.

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
)

// maxPart caps one chunk, since each is held in memory while it goes to S3.
const maxPart = 100 << 20

var chunkedKey = regexp.MustCompile(`^[0-9a-f]{32}/[^/]+$`)

type part struct {
	N    int64  `json:"n"`
	ETag string `json:"etag"`
}

func chunked(w http.ResponseWriter, r *http.Request) {
	cors(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}

	q := r.URL.Query()
	bucket := aws.String(os.Getenv("AWS_BUCKET"))
	svc := s3.New(newSession())

	if r.URL.Path == "/chunked/start" {
		if r.Method != "POST" {
			fail(w, 405, "POST to start a chunked upload.")
			return
		}
		name := path.Base(q.Get("filename"))
		if name == "." || name == "/" {
			fail(w, 422, `Missing "filename" query parameter.`)
			return
		}
		id := make([]byte, 16)
		rand.Read(id)
		key := hex.EncodeToString(id) + "/" + name
		out, err := svc.CreateMultipartUpload(&s3.CreateMultipartUploadInput{Bucket: bucket, Key: aws.String(key)})
		if err != nil {
			s3fail(w, err)
			return
		}
		reply(w, map[string]string{"key": key, "uploadId": *out.UploadId})
		return
	}

	key, uploadID := q.Get("key"), q.Get("uploadId")
	if !chunkedKey.MatchString(key) || uploadID == "" {
		fail(w, 422, `Missing or bad "key" or "uploadId" query parameter.`)
		return
	}

	switch r.URL.Path {
	case "/chunked/part":
		if r.Method != "PUT" {
			fail(w, 405, "PUT each chunk.")
			return
		}
		n, err := strconv.ParseInt(q.Get("n"), 10, 64)
		if err != nil || n < 1 || n > 10000 {
			fail(w, 422, `"n" must be a part number from 1 to 10000.`)
			return
		}
		body, err := ioutil.ReadAll(http.MaxBytesReader(w, r.Body, maxPart))
		if err != nil {
			fail(w, 413, fmt.Sprintf("Chunks can be at most %d MB.", maxPart>>20))
			return
		}
		out, err := svc.UploadPart(&s3.UploadPartInput{
			Bucket: bucket, Key: aws.String(key), UploadId: aws.String(uploadID),
			PartNumber: aws.Int64(n), Body: bytes.NewReader(body),
		})
		if err != nil {
			s3fail(w, err)
			return
		}
		reply(w, part{N: n, ETag: *out.ETag})

	case "/chunked/finish":
		if r.Method != "POST" {
			fail(w, 405, "POST to finish a chunked upload.")
			return
		}
		var parts []part
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&parts); err != nil || len(parts) == 0 {
			fail(w, 422, `Body must be a JSON list of {"n", "etag"} for every part.`)
			return
		}
		sort.Slice(parts, func(i, j int) bool { return parts[i].N < parts[j].N })
		done := make([]*s3.CompletedPart, len(parts))
		for i, p := range parts {
			done[i] = &s3.CompletedPart{PartNumber: aws.Int64(p.N), ETag: aws.String(p.ETag)}
		}
		out, err := svc.CompleteMultipartUpload(&s3.CompleteMultipartUploadInput{
			Bucket: bucket, Key: aws.String(key), UploadId: aws.String(uploadID),
			MultipartUpload: &s3.CompletedMultipartUpload{Parts: done},
		})
		if err != nil {
			s3fail(w, err)
			return
		}
		fmt.Fprint(w, *out.Location)

	case "/chunked/abort":
		if r.Method != "POST" {
			fail(w, 405, "POST to abort a chunked upload.")
			return
		}
		if _, err := svc.AbortMultipartUpload(&s3.AbortMultipartUploadInput{
			Bucket: bucket, Key: aws.String(key), UploadId: aws.String(uploadID),
		}); err != nil {
			s3fail(w, err)
			return
		}
		w.WriteHeader(204)

	default:
		fail(w, 404, "Unknown chunked endpoint.")
	}
}

func reply(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	fmt.Fprintln(w, msg)
}

func s3fail(w http.ResponseWriter, err error) {
	log.Printf("chunked upload: %v", err)
	fail(w, 500, fmt.Sprintf("Unexpected internal error when storing file. %v", err))
}
