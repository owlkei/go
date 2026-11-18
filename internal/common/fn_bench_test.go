package common

import (
	"testing"
)

func BenchmarkGetFnName(b *testing.B) {
	handler := fnTestController{}.READ_users
	b.ReportAllocs()
	for b.Loop() {
		GetFuncName(handler)
	}
}

func BenchmarkParseFnNameToURL_Simple(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		ParseFuncNameToURL("READ_users", PathStyleKebab)
	}
}

func BenchmarkParseFnNameToURL_Complex(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		ParseFuncNameToURL(
			"UPDATE_products_BY_productId_AND_productRanks_OF_categories_BY_categoryId_AND_categoryRank_OF_shops_BY_shopId_AND_shopRanks_VERSION_V_12",
			PathStyleKebab,
		)
	}
}

func BenchmarkParseWSFuncNameToEvent(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		ParseWSFuncNameToEvent("SUBSCRIBE_chat_to_user_ANY")
	}
}

func BenchmarkWSFnToSubject(b *testing.B) {
	fns := []string{
		"SUBSCRIBE_chat_message", "SUBSCRIBE_chat_ANY", "SUBSCRIBE_chat_ALL", "SUBSCRIBE_ALL",
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, fn := range fns {
			ParseWSFuncNameToEvent(fn)
		}
	}
}
