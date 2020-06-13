package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
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

	log.Println("Listening on port " + port + "!")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fmt.Fprintln(w, `Usage: POST a multipart form to this endpoint with a file in the "file" form field.`)
		w.WriteHeader(400)
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		fmt.Fprintln(w, `File not found in "file" form field.`)
		w.WriteHeader(422)
		return
	}
	defer file.Close()

	fileBytes, _ := ioutil.ReadAll(file)

	// Hash the file
	h := sha256.New()
	if _, err := io.Copy(h, bytes.NewReader(fileBytes)); err != nil {
		log.Fatal(err)
	}

	// The session the S3 Uploader will use
	sess := session.Must(session.NewSession(&aws.Config{
		Region:      aws.String(os.Getenv("S3_REGION")),
		Credentials: credentials.NewStaticCredentials(os.Getenv("AWS_ID"), os.Getenv("AWS_SECRET"), ""),
	}))

	// Create an uploader with the session and default options
	uploader := s3manager.NewUploader(sess)

	// Upload the file to S3.
	uploadOutput, err := uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(os.Getenv("AWS_BUCKET")),
		Key:    aws.String(fmt.Sprintf("%x", h.Sum(nil)) + "/" + handler.Filename),
		Body:   bytes.NewReader(fileBytes),
	})
	if err != nil {
		fmt.Errorf("failed to upload file, %v", err)

		fmt.Fprintln(w, "Unexpected internal error when storing file.", err)
		w.WriteHeader(500)

		return
	}

	// return that we have successfully uploaded our file!
	fmt.Fprintf(w, uploadOutput.Location)
}
