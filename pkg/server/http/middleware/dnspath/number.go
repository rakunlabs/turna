package dnspath

import "slices"

type Number struct {
	Map   map[int]struct{}
	Slice []int
}

func NewNumber(length int) *Number {
	return &Number{
		Map:   make(map[int]struct{}, length),
		Slice: make([]int, 0, length),
	}
}

func (n *Number) Set(number int) {
	n.Map[number] = struct{}{}
}

func (n *Number) Delete(number int) {
	delete(n.Map, number)
}

func (n *Number) Order() {
	for number := range n.Map {
		n.Slice = append(n.Slice, number)
	}

	slices.Sort(n.Slice)
}

func (n *Number) Pop() int {
	if len(n.Slice) == 0 {
		return 0
	}

	number := n.Slice[0]
	n.Slice = n.Slice[1:]

	delete(n.Map, number)

	return number
}
