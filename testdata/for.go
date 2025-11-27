package test

func forInfinite() {
	var x []int
	for {
		x = append(x, 0)
	}
}

func forWhile() {
	var x []int
	for true {
		x = append(x, 0)
	}
}

func forIncZeroToMaxExclusive() {
	var x []int // want "Consider preallocating x with capacity 5$"
	for i := 0; i < 5; i++ {
		x = append(x, i)
	}
}

func forIncOneToMaxExclusive() {
	var x []int // want "Consider preallocating x with capacity 4$"
	for i := 1; i < 5; i++ {
		x = append(x, i)
	}
}

func forIncZeroToMaxInclusive() {
	var x []int // want "Consider preallocating x with capacity 6$"
	for i := 0; i <= 5; i++ {
		x = append(x, i)
	}
}

func forIncZeroToNotMax() {
	var x []int // want "Consider preallocating x with capacity 5$"
	for i := 0; i != 5; i++ {
		x = append(x, i)
	}
}

func forDecMaxToZeroExclusive() {
	var x []int // want "Consider preallocating x with capacity 5$"
	for i := 5; i > 0; i-- {
		x = append(x, i)
	}
}

func forDecMaxToOneExclusive() {
	var x []int // want "Consider preallocating x with capacity 4$"
	for i := 5; i > 1; i-- {
		x = append(x, i)
	}
}

func forDecMaxToZeroInclusive() {
	var x []int // want "Consider preallocating x with capacity 6$"
	for i := 5; i >= 0; i-- {
		x = append(x, i)
	}
}

func forDecMaxToNotZero() {
	var x []int // want "Consider preallocating x with capacity 5$"
	for i := 5; i != 0; i-- {
		x = append(x, i)
	}
}

func forIncZeroToMaxExcReverse() {
	var x []int // want "Consider preallocating x with capacity 5$"
	for i := 0; 5 > i; i++ {
		x = append(x, i)
	}
}

func forIncZeroToMaxIncReverse() {
	var x []int // want "Consider preallocating x with capacity 6$"
	for i := 0; 5 >= i; i++ {
		x = append(x, i)
	}
}

func forDecMaxToZeroExcReverse() {
	var x []int // want "Consider preallocating x with capacity 5$"
	for i := 5; 0 < i; i-- {
		x = append(x, i)
	}
}

func forDecMaxToZeroIncReverse() {
	var x []int // want "Consider preallocating x with capacity 6$"
	for i := 5; 0 <= i; i-- {
		x = append(x, i)
	}
}

func forIncZeroToVarExclusive() {
	n := 5
	var x []int // want "Consider preallocating x with capacity n$"
	for i := 0; i < n; i++ {
		x = append(x, i)
	}
}

func forIncOneToVarExclusive() {
	n := 5
	var x []int // want "Consider preallocating x with capacity n - 1$"
	for i := 1; i < n; i++ {
		x = append(x, i)
	}
}

func forIncVarNegativeOneToVarExclusive() {
	n := 5
	var x []int // want "Consider preallocating x with capacity n \\+ 1$"
	for i := -1; i < n; i++ {
		x = append(x, i)
	}
}

func forIncVarToMaxExclusive() {
	m := 0
	var x []int // want "Consider preallocating x with capacity 5 - m$"
	for i := m; i < 5; i++ {
		x = append(x, i)
	}
}

func forIncVarToMaxInclusive() {
	m := 0
	var x []int // want "Consider preallocating x with capacity 5 - m \\+ 1$"
	for i := m; i <= 5; i++ {
		x = append(x, i)
	}
}

func forIncVarToZeroExclusive() {
	m := -5
	var x []int // want "Consider preallocating x with capacity -m$"
	for i := m; i < 0; i++ {
		x = append(x, i)
	}
}

func forIncVarToVarExclusive() {
	m := 0
	n := 5
	var x []int // want "Consider preallocating x with capacity n - m$"
	for i := m; i < n; i++ {
		x = append(x, i)
	}
}

func forIncVarToVarInclusive() {
	m := 0
	n := 5
	var x []int // want "Consider preallocating x with capacity n - m \\+ 1$"
	for i := m; i <= n; i++ {
		x = append(x, i)
	}
}
