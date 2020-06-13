# bucky

simple utility for passing files around between different cloud utilities

1. upload a multipart form to https://bucky.hackclub.com with the file you want to upload in the `file` field of the multipart form
2. get a temporary URL to the file. valid for 24 hours.
3. use that temporary URL to upload the file to airtable / anywhere else.

all files uploaded are deleted 24 hours after creation per s3 bucket policy

\- zrl
