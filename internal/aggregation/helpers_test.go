package aggregation

var benchNoop = func(data any) any { return data }

var benchPredicate = func(data any) bool { return true }

var benchFalse = func(data any) bool { return false }

func addOne(d any) any { return d.(int) + 1 }

func timesTwo(d any) any { return d.(int) * 2 }

func tapNothing(d any) any { return nil }

func isTrue(d any) bool { return true }
