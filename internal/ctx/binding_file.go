package ctx

import (
	"mime/multipart"
	"reflect"

	"go.owlkei.com/owlkei/internal/slice"
)

func BindFile(f File, s any) (map[string][]*DataFile, any) {
	return binderOfValue(s).bindFile(f)
}

func (b *Binder) bindFile(f File) (map[string][]*DataFile, any) {
	ptr := reflect.New(b.typ)
	dst := ptr.Elem()
	filteredFile := map[string][]*DataFile{}

	for i := range b.fields {
		field := &b.fields[i]
		if field.file == fileNone {
			continue
		}
		headers, ok := f[field.tag]
		if !ok {
			continue
		}

		switch field.file {
		case filePtr:
			fileHeader, ok := slice.Get(headers, field.tagIndex)
			if !ok {
				continue
			}
			dataFile := newDataFile(fileHeader, field.tagIndex, 1, field.tag)
			filteredFile[field.tag] = []*DataFile{dataFile}
			dst.Field(field.index).Set(reflect.ValueOf(dataFile))

		case fileSlice:
			dataFiles := make([]*DataFile, len(headers))
			for index, fileHeader := range headers {
				dataFiles[index] = newDataFile(fileHeader, index, len(headers), field.tag)
			}
			filteredFile[field.tag] = dataFiles
			dst.Field(field.index).Set(reflect.ValueOf(dataFiles))
		}
	}

	return filteredFile, dst.Interface()
}

func newDataFile(fileHeader *multipart.FileHeader, index, total int, key string) *DataFile {
	return &DataFile{
		FileHeader: fileHeader,
		Index:      index,
		Size:       fileHeader.Size,
		Total:      total,
		Key:        key,
		Filename:   fileHeader.Filename,
		Type:       fileHeader.Header.Get("Content-Type"),
	}
}
