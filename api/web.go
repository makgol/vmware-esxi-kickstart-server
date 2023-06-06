package api

import (
	"fmt"
	"io"
	"kickstart/config"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"text/template"

	"go.uber.org/zap"
)

type ErrorTemplateData struct {
	Title       string
	Message     string
	Description string
	Error       string
}

func errorResponseHandler(w http.ResponseWriter, form ErrorTemplateData, errcode int) error {
	errorform := `
	<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<meta http-equiv="refresh" content="10; url=./">
		<title>{{.Title}}</title>
	</head>
	<body>
		<h1>{{.Message}}</h1>
		<p>{{.Description}}</p>
		<p>{{.Error}}</p>
		<p>After 10 seconds, it will automatically redirect to TOP page.</p>
		<a href="/">Back to upload form</a>
	</body>
	</html>
	`
	tmpl, err := template.New("error").Parse(errorform)
	if err != nil {
		log.Printf("Error parsing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return err
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(errcode)
	err = tmpl.Execute(w, form)
	if err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return err
	}
	return nil
}

func (s *Server) getUploadFileHandler(config *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			form := ErrorTemplateData{
				Title:       "Method not allowed",
				Message:     "Method not allowed",
				Description: fmt.Sprintf("%s method is not allowed. POST request is supported.", r.Method),
				Error:       "",
			}
			err := errorResponseHandler(w, form, http.StatusMethodNotAllowed)
			if err != nil {
				s.logger.Error("error raised response handler", zap.Error(err))
			}
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			s.logger.Error("error retrieving the file", zap.Error(err))
			form := ErrorTemplateData{
				Title:       "Error retrieving the file",
				Message:     "Error retrieving the file",
				Description: "Failed retrieving the file. Please confirm the following error message.",
				Error:       err.Error(),
			}
			err := errorResponseHandler(w, form, http.StatusBadRequest)
			if err != nil {
				s.logger.Error("error raised response handler", zap.Error(err))
			}
			return
		}
		defer file.Close()

		if filepath.Ext(header.Filename) != ".iso" && filepath.Ext(header.Filename) != ".zip" {
			form := ErrorTemplateData{
				Title:       "Upload Error",
				Message:     "Invalid File Type",
				Description: "This file is not an `.iso` file. Only `.iso` files are supported.",
				Error:       "",
			}
			err := errorResponseHandler(w, form, http.StatusBadRequest)
			if err != nil {
				s.logger.Error("error raised response handler", zap.Error(err))
			}
			return
		}

		out, err := os.Create(filepath.Join(s.FileRootDirInfo.UploadedISODirPath, header.Filename))
		if err != nil {
			s.logger.Error("error creating the file", zap.Error(err))
			form := ErrorTemplateData{
				Title:       "Error creating the uploaded file",
				Message:     "Error creating the uploaded file",
				Description: "Failed creating the file. Please confirm the following error message.",
				Error:       err.Error(),
			}
			err := errorResponseHandler(w, form, http.StatusInternalServerError)
			if err != nil {
				s.logger.Error("error raised response handler", zap.Error(err))
			}
			return
		}
		defer out.Close()

		_, err = io.Copy(out, file)
		if err != nil {
			s.logger.Error("error saving the file", zap.Error(err))
			form := ErrorTemplateData{
				Title:       "Error saving the file",
				Message:     "Error saving the file",
				Description: "Failed saving the file. Please confirm the following error message.",
				Error:       err.Error(),
			}
			err := errorResponseHandler(w, form, http.StatusInternalServerError)
			if err != nil {
				s.logger.Error("error raised response handler", zap.Error(err))
			}
			return
		}

		esxiFilePath := out.Name()
		if filepath.Ext(header.Filename) == ".zip" {
			esxiFilePath, err = s.zipToIso(config, esxiFilePath, header.Filename)
			if err != nil {
				s.logger.Error("error raised response handler", zap.Error(err))
			}
		}

		err = s.ExtractISOfiles(config, esxiFilePath, header.Filename)
		if err != nil {
			form := ErrorTemplateData{
				Title:       "Extract ISO Failed",
				Message:     "Invalid File",
				Description: "Failed extracting ISO. Please confirm that the ISO file is a correct ESXi ISO.",
				Error:       err.Error(),
			}
			err := errorResponseHandler(w, form, http.StatusBadRequest)
			if err != nil {
				s.logger.Error("error raised response handler", zap.Error(err))
			}
			return
		}

		s.logger.Info(fmt.Sprintf("file upload successfully %s", header.Filename))

		form := `
		<!DOCTYPE html>
		<html lang="en">
		<head>
			<meta charset="UTF-8">
			<meta name="viewport" content="width=device-width, initial-scale=1.0">
			<meta http-equiv="refresh" content="10; url=./">
			<title>File Upload</title>
		</head>
		<body>
			<h1>Upload a ESXi ISO file</h1>
			<p>File uploaded successfully: %v</p>
			<p>After 10 seconds, it will automatically redirect to TOP page.</p>
			<br>
			<a href="/">Back to upload form</a>
		</body>
		</html>
		`
		fmt.Fprintf(w, form, header.Filename)
	}
}

func (s *Server) uploadForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			s.logger.Error(fmt.Sprintf("method %s not allowed", r.Method))
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var uploadedFiles string
		filepath.Walk(s.FileRootDirInfo.BootFileDirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				s.logger.Error("could not find files", zap.Error(err))
				return err
			}
			if info.IsDir() && filepath.Dir(path) == s.FileRootDirInfo.BootFileDirPath {
				uploadedFiles += "<li>" + filepath.Base(path) + "</li>"
			}
			return nil
		})

		form := `
            <!DOCTYPE html>
            <html lang="en">
            <head>
                <meta charset="UTF-8">
                <meta name="viewport" content="width=device-width, initial-scale=1.0">
                <title>File Upload</title>
            </head>
            <body>
                <h1>Upload a ESXi ISO file</h1>
                <form action="/upload" method="post" enctype="multipart/form-data">
                    <input type="file" name="file" required>
                    <button type="submit">Upload</button>
                </form>
                <br>
                <h2>Uploaded files:</h2>
                <ul>
                %s
                </ul>
            </body>
            </html>
        `

		w.Write([]byte(fmt.Sprintf(form, uploadedFiles)))
	}
}
