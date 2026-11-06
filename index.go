package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	http.HandleFunc("/", upload)
	http.HandleFunc("/chunked/", chunked)

	log.Println("Listening on port " + port + "!")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// cors lets web pages upload straight to bucky and read the URL it returns.
func cors(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "POST, PUT, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type")
	h.Set("Access-Control-Max-Age", "86400")
}

// newSession is the session the S3 clients use.
func newSession() *session.Session {
	return session.Must(session.NewSession(&aws.Config{
		Region:      aws.String(os.Getenv("S3_REGION")),
		Credentials: credentials.NewStaticCredentials(os.Getenv("AWS_ID"), os.Getenv("AWS_SECRET"), ""),
	}))
}

func upload(w http.ResponseWriter, r *http.Request) {
	cors(w)

	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}

	if r.Method != "POST" {
		w.WriteHeader(400)
		fmt.Fprintln(w, `Usage: POST a multipart form to this endpoint with a file in the "file" form field.`)
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(422)
		fmt.Fprintln(w, `File not found in "file" form field.`)
		return
	}
	defer file.Close()

	fileBytes, err := ioutil.ReadAll(file)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintln(w, "Couldn't read the uploaded file.")
		return
	}

	// Hash the file
	sum := sha256.Sum256(fileBytes)

	// Create an uploader with the session and default options
	uploader := s3manager.NewUploader(newSession())

	// Upload the file to S3.
	uploadOutput, err := uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(os.Getenv("AWS_BUCKET")),
		Key:    aws.String(fmt.Sprintf("%x", sum) + "/" + handler.Filename),
		Body:   bytes.NewReader(fileBytes),
	})
	if err != nil {
		log.Printf("failed to upload file: %v", err)

		w.WriteHeader(500)
		fmt.Fprintln(w, "Unexpected internal error when storing file.", err)

		return
	}

	// return that we have successfully uploaded our file! Print, not Printf:
	// the URL is escaped, so a filename with a space has "%20" in it.
	fmt.Fprint(w, uploadOutput.Location)
}
