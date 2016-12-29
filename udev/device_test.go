package udev

import "testing"

func TestGetTtyNumber(t *testing.T) {
	sample := []struct {
		src string
		num int
	}{
		{"/dev/ttyUSB0", 0},
		{"/dev/ttyUSB1", 1},
	}
	for _, v := range sample {
		n, err := getttyNum(v.src)
		if err != nil {
			t.Fatal(err)
		}
		if n != v.num {
			t.Errorf("expected %d got 5d", v.num, n)
		}
	}
}

//func TestMonitor(t *testing.T) {
//m := New()
//err := m.run(context.Background())
//if err != nil {
//t.Error(err)
//}
//}
