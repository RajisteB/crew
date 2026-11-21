package mission

import "sort"

// waitCircles returns each group of two or more pieces that wait on each
// other (strongly connected components), members and groups in file order.
func waitCircles(pieces []Piece, index map[string]int) [][]string {
	n := len(pieces)
	next := make([][]int, n)
	for i, p := range pieces {
		for _, w := range p.WaitsFor {
			if j, ok := index[w]; ok && j != i {
				next[i] = append(next[i], j)
			}
		}
	}
	t := &tarjan{next: next, idx: make([]int, n), low: make([]int, n), on: make([]bool, n)}
	for i := range t.idx {
		t.idx[i] = -1
	}
	for i := 0; i < n; i++ {
		if t.idx[i] < 0 {
			t.visit(i)
		}
	}
	sort.Slice(t.groups, func(a, b int) bool { return t.groups[a][0] < t.groups[b][0] })
	out := make([][]string, len(t.groups))
	for g, members := range t.groups {
		for _, i := range members {
			out[g] = append(out[g], pieces[i].ID)
		}
	}
	return out
}

type tarjan struct {
	next     [][]int
	idx, low []int
	on       []bool
	stack    []int
	counter  int
	groups   [][]int
}

func (t *tarjan) visit(v int) {
	t.idx[v], t.low[v] = t.counter, t.counter
	t.counter++
	t.stack = append(t.stack, v)
	t.on[v] = true
	for _, w := range t.next[v] {
		if t.idx[w] < 0 {
			t.visit(w)
			t.low[v] = min(t.low[v], t.low[w])
		} else if t.on[w] {
			t.low[v] = min(t.low[v], t.idx[w])
		}
	}
	if t.low[v] != t.idx[v] {
		return
	}
	var group []int
	for {
		w := t.stack[len(t.stack)-1]
		t.stack = t.stack[:len(t.stack)-1]
		t.on[w] = false
		group = append(group, w)
		if w == v {
			break
		}
	}
	if len(group) > 1 {
		sort.Ints(group)
		t.groups = append(t.groups, group)
	}
}
