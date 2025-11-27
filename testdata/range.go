package test

import "sort"

func rangeZero() {
	var x []int
	for i := range 0 {
		x = append(x, i)
	}
}

func rangeEmptyString() {
	var x []int
	for i := range "" {
		x = append(x, i)
	}
}

func rangeInt() {
	var x []int // want "Consider preallocating x with capacity 5$"
	for i := range 5 {
		x = append(x, i)
	}
}

func rangeIntVar() {
	n := 5
	var x []int // want "Consider preallocating x with capacity n$"
	for i := range n {
		x = append(x, i)
	}
}

func rangeIntArg(n int) {
	var x []int // want "Consider preallocating x with capacity n$"
	for i := range n {
		x = append(x, i)
	}
}

func rangeString() {
	var x []int // want "Consider preallocating x with capacity 5$"
	for i := range "Hello" {
		x = append(x, i)
	}
}

func rangeStringVar() {
	s := "Hello"
	var x []int // want "Consider preallocating x with capacity len\\(s\\)$"
	for i := range s {
		x = append(x, i)
	}
}

func rangeStringArg(s string) {
	var x []int // want "Consider preallocating x with capacity len\\(s\\)$"
	for i := range s {
		x = append(x, i)
	}
}

func rangeSlice() {
	var a []int
	var x []int // want "Consider preallocating x with capacity len\\(a\\)$"
	for i := range a {
		x = append(x, i)
	}
}

func rangeArray() {
	var a [5]int
	var x []int // want "Consider preallocating x with capacity len\\(a\\)$"
	for i := range a {
		x = append(x, i)
	}
}

func rangeArrayPointer() {
	var a *[5]int
	var x []int // want "Consider preallocating x with capacity len\\(a\\)$"
	for i := range a {
		x = append(x, i)
	}
}

func rangeMap() {
	var m map[int]int
	var x []int // want "Consider preallocating x with capacity len\\(m\\)$"
	for i := range m {
		x = append(x, i)
	}
}

func rangeMultiple() {
	var x []int // want "Consider preallocating x with capacity 5 \\+ n \\+ len\\(s\\) \\+ \\(n - m \\+ 1\\)$"
	for i := range 5 {
		x = append(x, i)
	}
	n := 5
	for i := range n {
		x = append(x, i)
	}
	s := "Hello"
	for i := range s {
		x = append(x, i)
	}
	m := 0
	for i := m; i <= n; i++ {
		x = append(x, i)
	}
}

func rangeMultipleWithPartialUnresolvedCapacity() {
	var x []int // want "Consider preallocating x$"
	for i := range 5 {
		x = append(x, i)
	}
	var s sort.IntSlice
	for i := range s {
		x = append(x, i)
	}
}
