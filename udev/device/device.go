package device

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	stream  *events.Stream
}

// New returns a new Manager instance
func New() *Manager {
	return &Manager{}
}

// Init initializes the manager. This involves creating a new goroutine to watch
// over the changes detected by udev for any device interaction with the system.
//
// The only interesting device actions are add and reomove for adding and
// removing devices respctively.

func (m *Manager) run(ctx context.Context) error {
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
				m.RemoveDevice(ctx, dpath)
			}
		}
		wg.Done()
	}()
	wg.Wait()
	return nil

}

func (m *Manager) RemoveDevice(ctx context.Context, dpath string) {
}
func (m *Manager) startup(ctx context.Context) {
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
	e := &events.Event{Name: "add", Data: modem}
	m.stream.Send(e)
	return nil
}

func FindModem(ctx context.Context, d *udev.Device) (*db.Dongle, error) {
	name := filepath.Join("/dev", filepath.Base(d.Devpath()))
	if strings.Contains(name, "ttyUSB") {
		cfg := serial.Config{Name: name, Baud: 9600, ReadTimeout: 10 * time.Second}
		conn := &Conn{device: cfg}
		err := conn.Open()
		if err != nil {
			return nil, err
		}
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
	imei, err := getIMEI(c)
	if err != nil {
		return nil, err
	}
	imsi, err := getIMSI(c)
	if err != nil {
		return nil, err
	}
	m.IMEI = imei
	m.IMSI = imsi
	m.Path = c.device.Name
	return m, nil
}

func mustExec(duration time.Duration, c *Conn, cmd string) ([]byte, error) {
	ich := time.After(duration)
	for {
		select {
		case <-ich:
			return nil, errors.New("timed out")
		default:
			rst, err := c.Run(cmd)
			if err != nil {
				continue
			}
			return rst, nil
		}
	}
}

func getIMEI(c *Conn) (string, error) {
	o, err := mustExec(10*time.Second, c, "AT+GSN")
	if err != nil {
		return "", err
	}
	i, err := cleanResult(o)
	if err != nil {
		return "", err
	}
	im := string(i)
	if !isNumber(im) {
		return "", errors.New("IMEI not found")
	}
	fmt.Println("imei ", im)
	return im, nil
}

func getIMSI(c *Conn) (string, error) {
	o, err := mustExec(10*time.Second, c, "AT+CIMI")
	if err != nil {
		return "", err
	}
	i, err := cleanResult(o)
	if err != nil {
		return "", err
	}
	im := string(i)
	if !isNumber(im) {
		return "", errors.New("IMSI not found")
	}
	fmt.Println("imsi ", im)

	return im, nil
}

func isNumber(src string) bool {
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
		return nil, errors.New("command " + string(cmd) + " xeite without OK" + " got " + string(buf))
	}
	_ = c.port.Flush()
	_ = c.port.Close()
	c.isOpen = false
	return buf, nil
}

// Run helper for Exec that adds \r to the command
func (c *Conn) Run(cmd string) ([]byte, error) {
	return c.Exec(fmt.Sprintf("%s \r", cmd))
}
