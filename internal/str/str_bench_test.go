package str

import (
	"testing"
)

func BenchmarkRemoveSpace(b *testing.B) {
	input := "A B C D E F G H I J K L M N O P Q R S T U V W X Y Z a b c d e f g h i j k l m n o p q r s t u v w x y z"
	b.ReportAllocs()
	for b.Loop() {
		RemoveSpace(input)
	}
}

func BenchmarkEnclose_Dirty(b *testing.B) {
	input := "//users//profile//settings//"
	b.ReportAllocs()
	for b.Loop() {
		Enclose(input, '/')
	}
}

func BenchmarkEnclose_Canonical(b *testing.B) {
	input := "/users/all/"
	b.ReportAllocs()
	for b.Loop() {
		Enclose(input, '/')
	}
}

func BenchmarkRemoveDup_Dirty(b *testing.B) {
	input := "/**/school**/***/***/{subjectId}/***"
	b.ReportAllocs()
	for b.Loop() {
		RemoveDup(input, '*')
	}
}

func BenchmarkRemoveDup_Clean(b *testing.B) {
	input := "/static/css/app.min.css"
	b.ReportAllocs()
	for b.Loop() {
		RemoveDup(input, '/')
	}
}
