package test

func sliceAssignEmptyLit() {
	x := []int{} // want "Consider preallocating x with capacity 5$"
	for i := range "Hello" {
		x = append(x, i)
	}
}

func sliceAssignEmptyMake() {
	x := make([]int, 0) // want "Consider preallocating x with capacity 5$"
	for i := range "Hello" {
		x = append(x, i)
	}
}

func sliceAssignNilConvert() {
	x := []int(nil) // want "Consider preallocating x with capacity 5$"
	for i := range "Hello" {
		x = append(x, i)
	}
}

func sliceVarAssignEmptyLit() {
	var x = []int{} // want "Consider preallocating x with capacity 5$"
	for i := range "Hello" {
		x = append(x, i)
	}
}

func sliceVarAssignEmptyMake() {
	var x = make([]int, 0) // want "Consider preallocating x with capacity 5$"
	for i := range "Hello" {
		x = append(x, i)
	}
}

func sliceVarAssignNilConvert() {
	var x = []int(nil) // want "Consider preallocating x with capacity 5$"
	for i := range "Hello" {
		x = append(x, i)
	}
}

func sliceAlreadyInitialized() {
	x := []int{1, 2, 3} // want "Consider preallocating x with capacity 8$"
	for i := range "Hello" {
		x = append(x, i)
	}
}

func sliceVarAlreadyInitialized() {
	var x = []int{1, 2, 3} // want "Consider preallocating x with capacity 8$"
	for i := range "Hello" {
		x = append(x, i)
	}
}

func sliceAlreadyInitializedWithoutAppend() {
	x := []int{1, 2, 3}
	_ = x
}

func sliceVarTypedAlreadyInitialized() {
	var x []int = []int{1, 2, 3} // want "Consider preallocating x with capacity 8$"
	for i := range "Hello" {
		x = append(x, i)
	}
}

func sliceAlreadyFilled() {
	x := make([]int, 5) // want "Consider preallocating x with capacity 10$"
	for i := range "Hello" {
		x = append(x, i)
	}
}

func sliceVarAlreadyFilled() {
	var x = make([]int, 5) // want "Consider preallocating x with capacity 10$"
	for i := range "Hello" {
		x = append(x, i)
	}
}

func sliceVarTypedAlreadyFilled() {
	var x []int = make([]int, 5) // want "Consider preallocating x with capacity 10$"
	for i := range "Hello" {
		x = append(x, i)
	}
}

func sliceVarAllocatedBeforeAppend() {
	var x []int
	x = make([]int, 0, 1)
	x = append(x, 0)
}

func sliceVarAllocatedAfterAppend() {
	var x []int
	x = append(x, 0)
	x = make([]int, 0, 1)
}

func sliceVarReassignedBeforeAppend(y []int) {
	var x []int
	x = y
	x = append(x, 0)
}

func sliceVarReassignedAfterAppend(y []int) {
	var x []int
	x = append(x, 0)
	x = y
}

func sliceVarReused() {
	var x []int // want "Consider preallocating x with capacity 1$"
	x = append(x, 0)
	x = nil // want "Consider preallocating x with capacity 1$"
	x = append(x, 0)
	x = []int{} // want "Consider preallocating x with capacity 1$"
	x = append(x, 0)
	x = []int(nil) // want "Consider preallocating x with capacity 1$"
	x = append(x, 0)
	x = make([]int, 0) // want "Consider preallocating x with capacity 1$"
	x = append(x, 0)
}

func multipleVarNames() {
	var x, y []int // want "Consider preallocating x with capacity 5$" "Consider preallocating y with capacity 5$"
	for i := range 5 {
		x = append(x, i)
		y = append(y, i)
	}
}
