package task

import (
	"reflect"
	"sync"
	"testing"

	"github.com/Leoj030/go-luau/luau"
)

func TestStatesRunInParallel(t *testing.T) {
	bc, err := luau.Compile(`
		local n = 0
		for i = 1, 200000 do n = n + i end
		counter = (counter or 0) + 1
		return n, counter, hello()`)
	if err != nil {
		t.Fatal(err)
	}
	// One value, flattened once, pushed into every state.
	shared, err := luau.Encode(map[string]any{"list": []int{1, 2, 3}, "name": "shared"})
	if err != nil {
		t.Fatal(err)
	}

	const workers = 8
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			L, err := luau.NewState()
			if err != nil {
				t.Error(err)
				return
			}
			defer L.Close()
			L.Register("hello", func(c *luau.State) (int, error) { return 1, c.Push("hi") })

			for i := 1; i <= 50; i++ {
				if err := L.Load(bc, "=worker"); err != nil {
					t.Error(err)
					return
				}
				res, err := L.Call()
				if err != nil {
					t.Error(err)
					return
				}
				// Each state counts on its own, because its globals are its own.
				if len(res) != 3 || res[1] != float64(i) || res[2] != "hi" {
					t.Errorf("run %d: got %v", i, res)
					return
				}

				if err := L.PushEncoded(shared); err != nil {
					t.Error(err)
					return
				}
				var got map[string]any
				if err := L.Decode(-1, &got); err != nil || !reflect.DeepEqual(got["list"], []any{1.0, 2.0, 3.0}) {
					t.Errorf("shared value: got %v, %v", got, err)
				}
				L.Pop(1)
			}
		}()
	}
	wg.Wait()
}