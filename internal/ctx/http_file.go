package ctx

import (
	"mime/multipart"
	"strings"

	"go.owlky.com/owlky/exception"
)

type fileValidator interface {
	IsValid(*DataFile) bool
}

type fileHandler interface {
	Store(*DataFile, multipart.File)
}

type DataFile struct {
	*multipart.FileHeader
	Index    int
	Size     int64
	Total    int
	Key      string
	Filename string
	Type     string
	Dest     string
}

type File map[string][]*multipart.FileHeader

func (c *HTTPContext) File() File {
	if c.file != nil {
		return c.file
	}

	if c.Request.MultipartForm == nil && strings.EqualFold(c.contentType(), multipartFormData) {
		c.parseMultipartForm()
	}
	if mf := c.Request.MultipartForm; mf != nil && mf.File != nil {
		c.file = File(mf.File)
	} else {
		c.file = File{}
	}
	return c.file
}

func (files File) Bind(s any) any {
	filteredFile, newStructuredData := BindFile(files, s)

	if fileValidator, ok := s.(fileValidator); ok {
		for _, dataFileArr := range filteredFile {
			for _, dataFile := range dataFileArr {
				if !fileValidator.IsValid(dataFile) {
					panic(exception.BadRequestException(
						"Invalid file upload. Please make sure '" + dataFile.Filename + "' is a supported file",
					))
				}
			}
		}
	}

	if fileHandler, ok := s.(fileHandler); ok {
		for _, dataFileArr := range filteredFile {
			for _, dataFile := range dataFileArr {
				storeFile(fileHandler, dataFile)
			}
		}
	}

	return newStructuredData
}

func storeFile(handler fileHandler, dataFile *DataFile) {
	src, err := dataFile.Open()
	if err != nil {
		panic(exception.BadRequestException("Invalid file upload", err))
	}
	defer func() { _ = src.Close() }()
	handler.Store(dataFile, src)
}
