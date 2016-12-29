package db

//Dongle holds information about device dongles. This relies on combination from
//the information provided by udev and information that is gathered by talking
//to the device serial port directly.
type Dongle struct {
	IMEI        string
	IMSI        string
	Path        string
	IsSymlinked bool
	Properties  map[string]string
}
