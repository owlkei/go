package common

import "testing"

func BenchmarkGetPrefixes_NoHandlers(b *testing.B) {
	r := &HTTP{}
	r.Prefix("/api/v1")
	r.Prefix("/internal")
	b.ReportAllocs()
	for b.Loop() {
		r.GetPrefixes()
	}
}

func BenchmarkGetPrefixes_WithHandlers(b *testing.B) {
	r := &HTTP{}
	r.Prefix("/api", fnTestController{}.READ_users, fnTestController{}.CREATE_orders)
	b.ReportAllocs()
	for b.Loop() {
		r.GetPrefixes()
	}
}

func BenchmarkAddPrefixesToRoute(b *testing.B) {
	r := &HTTP{}
	r.Prefix("/api/v1")
	r.Prefix("/internal")
	prefixes := r.GetPrefixes()
	b.ReportAllocs()
	for b.Loop() {
		r.addPrefixesToRoute("/users/", "READ_users", prefixes)
	}
}
