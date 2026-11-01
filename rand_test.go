package mendoza

import "math/rand"

func randVal(r *rand.Rand, depth int) interface{} {
	k := r.Intn(7)
	if depth > 3 {
		k = r.Intn(4)
	}
	switch k {
	case 0:
		return nil
	case 1:
		return r.Intn(2) == 0
	case 2:
		return float64(r.Intn(5))
	case 3:
		return []string{"a", "b", "hello", "hello world", "héllo"}[r.Intn(5)]
	case 4, 5:
		m := map[string]interface{}{}
		for i, n := 0, r.Intn(5); i < n; i++ {
			m[string(rune('a'+r.Intn(6)))] = randVal(r, depth+1)
		}
		return m
	default:
		a := []interface{}{}
		for i, n := 0, r.Intn(5); i < n; i++ {
			a = append(a, randVal(r, depth+1))
		}
		return a
	}
}

func mutate(r *rand.Rand, v interface{}, depth int) interface{} {
	if r.Intn(4) == 0 {
		return randVal(r, depth)
	}
	switch v := v.(type) {
	case map[string]interface{}:
		m := map[string]interface{}{}
		for k, x := range v {
			if r.Intn(6) != 0 {
				m[k] = mutate(r, x, depth+1)
			}
		}
		if r.Intn(3) == 0 {
			m[string(rune('a'+r.Intn(6)))] = randVal(r, depth+1)
		}
		return m
	case []interface{}:
		a := []interface{}{}
		for _, x := range v {
			if r.Intn(6) != 0 {
				a = append(a, mutate(r, x, depth+1))
			}
		}
		if r.Intn(3) == 0 {
			a = append(a, randVal(r, depth+1))
		}
		if len(a) > 1 && r.Intn(3) == 0 {
			i, j := r.Intn(len(a)), r.Intn(len(a))
			a[i], a[j] = a[j], a[i]
		}
		return a
	case string:
		if r.Intn(2) == 0 {
			return v + "x"
		}
	}
	return v
}
