package test

func appendNothing() {
	var x []int
	for range "Hello" {
		x = append(x)
	}
}

func appendToAnother() {
	var x []int
	var y []int
	for i := range "Hello" {
		x = append(y, i)
	}
	_ = x
}

func appendEllipsis() {
	var nums []int
	var x []int
	for range "Hello" {
		x = append(x, nums...)
	}
}

func appendNormalAndEllipsis() {
	var nums []int
	var x []int
	for i := range "Hello" {
		x = append(x, i)
		x = append(x, nums...)
	}
}

func appendMultipleCalls() {
	var x []int // want "Consider preallocating x with capacity 10$"
	for i := range 5 {
		x = append(x, i)
		x = append(x, i)
	}
}

func appendMultipleArgs() {
	var x []int // want "Consider preallocating x with capacity 10$"
	for i := range 5 {
		x = append(x, i, i)
	}
}

func appendMultipleRangeIntVar() {
	n := 5
	var x []int // want "Consider preallocating x with capacity 2 \\* n$"
	for i := range n {
		x = append(x, i, i)
	}
}

func appendMultipleRangeStringVar() {
	s := "Hello"
	var x []int // want "Consider preallocating x with capacity 2 \\* len\\(s\\)$"
	for i := range s {
		x = append(x, i, i)
	}
}
