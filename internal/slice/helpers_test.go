package slice

var benchStrings = func() []string {
	s := make([]string, 1024)
	for i := range s {
		if i%2 == 0 {
			s[i] = "keep"
		} else {
			s[i] = "drop"
		}
	}
	return s
}()

var benchInts = func() []int {
	s := make([]int, 1024)
	for i := range s {
		s[i] = i
	}
	return s
}()

var benchNumStrs = func() []string {
	s := make([]string, 1024)
	for i := range s {
		s[i] = "42"
	}
	return s
}()
