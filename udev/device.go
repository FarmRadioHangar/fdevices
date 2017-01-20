package udev

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FarmRadioHangar/devices/db"
	"github.com/FarmRadioHangar/devices/events"
	"github.com/jochenvg/go-udev"
	"github.com/tarm/serial"
)

var modemCommands = struct {
	IMEI, IMSI string
}{
	"AT+GSN", "AT+CIMI",
}

// Manager manages devices that are plugged into the system. It supports auto
// detection of devices.
//
// Serial ports are opened each for a device, and a clean API for communicating
// is provided via Read, Write and Flush methods.
//
// The devices are monitored via udev, and any changes that requires reloading
// of the  ports are handled by reloading the ports to the devices.
//
// This is safe to use concurrently in multiple goroutines
type Manager struct {
	monitor *udev.Monitor
	db      *sql.DB
	stream  *events.Stream
}

// New returns a new Manager instance
func New(db *sql.DB, s *events.Stream) *Manager {
	return &Manager{stream: s, db: db}
}

// Init initializes the manager. This involves creating a new goroutine to watch
// over the changes detected by udev for any device interaction with the system.
//
// The only interesting device actions are add and reomove for adding and
// removing devices respctively.

func (m *Manager) Run(ctx context.Context) error {
	u := udev.Udev{}
	monitor := u.NewMonitorFromNetlink("udev")
	monitor.FilterAddMatchTag("systemd")
	done := make(chan struct{})
	ch, err := monitor.DeviceChan(done)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
	stop:
		for {
			select {
			case <-ctx.Done():
				done <- struct{}{}
				break stop
			}
		}
		wg.Done()
	}()
	go func() {
		fmt.Println("starting listening for events")
		for d := range ch {
			dpath := filepath.Join("/dev", filepath.Base(d.Devpath()))
			switch d.Action() {
			case "add":
				m.AddDevice(ctx, d)
			case "remove":
				err := m.RemoveDevice(ctx, dpath)
				if err != nil {
					fmt.Println(err)
				}
			}
		}
		wg.Done()
	}()
	wg.Wait()
	return nil

}

func (m *Manager) RemoveDevice(ctx context.Context, dpath string) error {
	d, err := db.GetDongle(m.db, dpath)
	if err != nil {
		return err
	}
	c, err := db.GetSymlinkCandidate(m.db, d.IMEI)
	if err != nil {
		return db.RemoveDongle(m.db, d)
	}
	err = db.RemoveDongle(m.db, c)
	if err != nil {
		return err
	}
	fmt.Println("removed donge", c.IMEI)
	m.unlink(c)
	return nil
}
func (m *Manager) Startup(ctx context.Context) {
	u := udev.Udev{}
	e := u.NewEnumerate()
	e.AddMatchIsInitialized()
	e.AddMatchTag("systemd")
	devices, _ := e.Devices()
	for i := 0; i < len(devices); i++ {
		m.AddDevice(ctx, devices[i])
	}
}

// AddDevice adds device name to the manager
//
// WARNING: The way modems are picked is a hack. It asserts that the modem with
// the lowest tty number is the control modem( Which I'm not so sure is always
// correct).
//
// TODO: comeup with a proper way to identify modems
func (m *Manager) AddDevice(ctx context.Context, d *udev.Device) error {
	err := m.addDevice(ctx, d)
	if err != nil {
		return err
	}
	return nil
}
func (m *Manager) addDevice(ctx context.Context, d *udev.Device) error {
	modem, err := FindModem(ctx, d)
	if err != nil {
		return err
	}
	modem.Properties = d.Properties()
	e := &events.Event{Name: "add", Data: modem}
	_, err = db.GetSymlinkCandidate(m.db, modem.IMEI)
	if err != nil {
		_, err = db.GetDongleByIMEI(m.db, modem.IMEI)
		if err != nil {
			m.stream.Send(e)
		}
	}
	err = db.CreateDongle(m.db, modem)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Printf("found dongle imei:%s imsi:%s path:%s \n",
		modem.IMEI, modem.IMSI, modem.Path,
	)
	go m.Symlink(modem)
	return nil
}
func (m *Manager) symlink(d *db.Dongle) {
	n := fmt.Sprintf("/dev/%s.imei", d.IMEI)
	_ = syscall.Unlink(n)
	err := os.Symlink(d.Path, n)
	if err != nil {
		fmt.Printf("devices-symlinks :  %v \n", err)
		return
	}
	fmt.Printf("device-symlink: %s --> %s\n", n, d.Path)
	i := fmt.Sprintf("/dev/%s.imsi", d.IMSI)
	_ = syscall.Unlink(i)
	err = os.Symlink(d.Path, i)
	if err != nil {
		fmt.Printf("devices-symlinks :  %v \n", err)
	}
	d.IsSymlinked = true
	err = db.UpdateDongle(m.db, d)
	if err != nil {
		fmt.Printf("ERROR; %v \n", err)
	} else {
		e := &events.Event{Name: "update", Data: d}
		m.stream.Send(e)
	}
	fmt.Printf("device-symlink: %s --> %s\n", i, d.Path)
}

func (m *Manager) unlink(d *db.Dongle) {
	n := fmt.Sprintf("/dev/%s.imei", d.IMEI)
	_ = syscall.Unlink(n)
	fmt.Printf("device-unlink: %s --> %s\n", n, d.Path)
	i := fmt.Sprintf("/dev/%s.imsi", d.IMSI)
	_ = syscall.Unlink(i)
	fmt.Printf("device-unlink: %s --> %s\n", i, d.Path)
	e := &events.Event{Name: "remove", Data: d}
	m.stream.Send(e)
}
func (m *Manager) Symlink(d *db.Dongle) {
	if d.IMSI == "" {
		return
	}
	c, err := db.GetSymlinkCandidate(m.db, d.IMEI)
	if err != nil {
		return
	}
	if !c.IsSymlinked {
		m.symlink(c)
	}
}

//FindDongle thic=s checks if the udev Device is a donge. We are only interested
//in dongle that we can communicate with via serial port.
//
// For a plugged in 3g dongle, three devices are seen by udev. They are
// registered in three different tty. For isntance tty0,tty1,tty2. It is not
// obvious to know which is the command tty.
//
// Only two tty's are candidates for the command dongle. The criteria of picking
// the right candidate is based on whether we can get IMEI and IMSI number from
// the tty.
func FindModem(ctx context.Context, d *udev.Device) (*db.Dongle, error) {
	name := filepath.Join("/dev", filepath.Base(d.Devpath()))
	if strings.Contains(name, "ttyUSB") {
		cfg := serial.Config{Name: name, Baud: 9600, ReadTimeout: 10 * time.Second}
		conn := &Conn{device: cfg}
		err := conn.Open()
		if err != nil {
			return nil, err
		}
		defer conn.Close()
		modem, err := NewModem(ctx, conn)
		if err != nil {
			return nil, err
		}
		return modem, nil
	}
	return nil, errors.New("not a dongle")
}

func getttyNum(tty string) (int, error) {
	b := filepath.Base(tty)
	b = strings.TrimPrefix(b, "ttyUSB")
	return strconv.Atoi(b)
}

func NewModem(ctx context.Context, c *Conn) (*db.Dongle, error) {
	m := &db.Dongle{}
	imei, ati, err := getIMEI(c)
	if err != nil {
		return nil, err
	}
	imsi, err := getIMSI(c)
	if err != nil {
		fmt.Println(err)
	}
	m.IMEI = imei
	m.ATI = ati
	m.IMSI = imsi
	m.Path = c.device.Name
	i, err := getttyNum(m.Path)
	if err != nil {
		return nil, err
	}
	m.TTY = i
	return m, nil
}

func mustExec(duration time.Duration, c *Conn, cmd string) ([]byte, error) {
	ich := time.After(duration)
	tk := time.NewTicker(time.Second)
	defer tk.Stop()
	for {
		select {
		case <-ich:
			return c.Run(cmd)
		case <-tk.C:
			rst, err := c.Run(cmd)
			if err != nil {
				continue
			}
			return rst, nil
		}

	}
}

func getIMEI(c *Conn) (string, string, error) {
	o, err := mustExec(10*time.Second, c, "ATI")
	if err != nil {
		return "", "", err
	}
	ati := string(o)
	im := clearIMEI(string(o))
	if !isNumber(im) {
		return "", "", errors.New("IMEI not found")
	}
	return im, ati, nil
}

func clearIMEI(src string) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return src
	}
	im := "IMEI:"
	gap := "+GCAP"
	i := strings.Index(src, im)
	if i == -1 {
		return ""
	}
	g := strings.Index(src, gap)
	if i == -1 {
		return ""
	}
	return strings.TrimSpace(src[i+len(im) : g])
}

func getIMSI(c *Conn) (string, error) {
	o, err := mustExec(10*time.Second, c, "AT+CIMI")
	if err != nil {
		return "", err
	}
	fmt.Println(string(o))
	i, err := cleanResult(o)
	if err != nil {
		return "", err
	}
	im := string(i)
	if !isNumber(im) {
		return "", errors.New("IMSI not found")
	}
	return im, nil
}

func isNumber(src string) bool {
	if src == "" {
		return false
	}
	for _, v := range src {
		if !unicode.IsDigit(v) {
			return false
		}
	}
	return true
}

func cleanResult(src []byte) ([]byte, error) {
	i := bytes.Index(src, []byte("OK"))
	if i == -1 {
		return nil, errors.New("not okay")
	}
	ns := bytes.TrimSpace(src[:i])
	ch, _ := utf8.DecodeRune(ns)
	if unicode.IsLetter(ch) {
		at := bytes.Index(ns, []byte("AT"))
		if at != -1 {
			i := bytes.IndexRune(ns[at:], '\r')
			if i > 0 {
				return bytes.TrimSpace(ns[at+i:]), nil
			}
		}
	}
	return ns, nil
}

//Close shuts down the device manager. This makes sure the udev monitor is
//closed and all goroutines are properly exited.
func (m *Manager) Close() {
}

// Conn is a device serial connection
type Conn struct {
	device serial.Config
	imei   string
	port   *serial.Port
	isOpen bool
}

// Open opens a serial port to the undelying device
func (c *Conn) Open() error {
	p, err := serial.OpenPort(&c.device)
	if err != nil {
		return err
	}
	c.port = p
	c.isOpen = true
	return nil
}

// Close closes the port helt by *Conn.
func (c *Conn) Close() error {
	if c.isOpen {
		return c.port.Close()
	}
	return nil
}

// Write wites b to the serieal port
func (c *Conn) Write(b []byte) (int, error) {
	return c.port.Write(b)
}

// Read reads from serial port
func (c *Conn) Read(b []byte) (int, error) {
	return c.port.Read(b)
}

// Exec sends the command over serial port and rrturns the response. If the port
// is closed it is opened  before sending the command.
func (c *Conn) Exec(cmd string) ([]byte, error) {
	if !c.isOpen {
		err := c.Open()
		if err != nil {
			return nil, err
		}
	}
	_, err := c.Write([]byte(cmd))
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 128)
	_, err = c.Read(buf)
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(buf, []byte("OK")) {
		return nil, errors.New(string(buf))
	}
	_ = c.port.Flush()
	c.isOpen = false
	return buf, nil
}

// Run helper for Exec that adds \r to the command
func (c *Conn) Run(cmd string) ([]byte, error) {
	return c.Exec(fmt.Sprintf("%s\r\n", cmd))
}
