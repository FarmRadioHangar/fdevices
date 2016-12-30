package db

import "testing"

func TestDb(t *testing.T) {
	q, err := DB()
	if err != nil {
		t.Fatal(err)
	}

	sample := []*Dongle{
		{
			IMEI: "000000",
			IMSI: "000001",
			Path: "/dev/tty0",
		},
		{
			IMEI: "000001",
			IMSI: "000002",
			Path: "/dev/tty1",
		},
		{
			IMEI: "000002",
			IMSI: "000003",
			Path: "/dev/tty2",
		},
	}

	for _, v := range sample {
		err = CreateDongle(q, v)
		if err != nil {
			t.Error(err)
		}
	}

	for _, v := range sample {
		d, err := GetDongle(q, v.Path)
		if err != nil {
			t.Error(err)
		}
		if d != nil {
			if d.IMEI != v.IMEI {
				t.Errorf("expected %s got %s", v.IMEI, d.IMEI)
			}
		}
	}
	a, err := GetAllDongles(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(sample) {
		t.Errorf("expected %d got %d", len(sample), len(a))
	}
}
