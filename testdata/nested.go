package test

// nested statement blocks should be processed to any depth

func nest() {
	{
		var x []int // want "Consider preallocating x with capacity 5$"
		for i := range "Hello" {
			x = append(x, i)
		}

		if true {
			var y []int // want "Consider preallocating y with capacity 5$"
			for i := range "Hello" {
				y = append(y, i)
			}

			for {
				var z []int // want "Consider preallocating z with capacity 5$"
				for i := range "Hello" {
					z = append(z, i)
				}
				break
			}
		}
	}
}
