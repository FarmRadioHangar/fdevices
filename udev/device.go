package udev

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FarmRadioHangar/fdevices/db"
	"github.com/FarmRadioHangar/fdevices/events"
	"github.com/FarmRadioHangar/fdevices/log"
	"github.com/jochenvg/go-udev"
	"github.com/tarm/serial"
)

var modemCommands = struct {
	IMEI, IMSI string
}{
	"AT+GSN", "AT+CIMI",
}

// MaxAttempt is the maximum numbet of attempts to find imsi and imsi
const MaxAttempt = 3

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

// Run  initializes the manager. This involves creating a new goroutine to watch
// over the changes detected by udev for any device interaction with the system.
//
// The only interesting device actions are add and reomove for adding and
// removing devices respctively.
func (m *Manager) Run(ctx context.Context) error {
	log.Info("running the manager")
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
				log.Info("stopping manager")
				break stop
			}
		}
		wg.Done()
	}()
	go func() {
		log.Info("starting listening for events")
		for d := range ch {
			dpath := filepath.Join("/dev", filepath.Base(d.Devpath()))
			switch d.Action() {
			case "add":
				log.Info("received add event for %s", dpath)
				m.AddDevice(ctx, d)
			case "remove":
				log.Info("received remove event for %s", dpath)
				err := m.RemoveDevice(ctx, dpath)
				if err != nil {
					log.Error(err.Error())
				}
			}
		}
		wg.Done()
	}()
	wg.Wait()
	log.Info("exit running manager")
	return nil

}

// RemoveDevice removes the dongle which has been tracked by the manager
func (m *Manager) RemoveDevice(ctx context.Context, dpath string) error {
	d, err := db.GetDongle(m.db, dpath)
	if err != nil {
		return nil
	}
	c, err := db.GetSymlinkCandidate(m.db, d.IMEI)
	if err != nil {
		e := &events.Event{Name: "remove", Data: d}
		m.stream.Send(e)
		log.Info("removed dongle with imei %s", d.IMEI)
		return db.RemoveDongle(m.db, d)
	}
	err = db.RemoveDongle(m.db, c)
	if err != nil {
		return err
	}
	log.Info("successful removed gongle with imei %s", c.IMEI)
	m.unlink(c)
	return nil
}

// Startup starts the manager for the first time. This deals with devices
// that are already in the system by the time the manager was started
func (m *Manager) Startup(ctx context.Context) {
	u := udev.Udev{}
	e := u.NewEnumerate()
	e.AddMatchIsInitialized()
	e.AddMatchTag("systemd")
	devices, _ := e.Devices()
	for i := 0; i < len(devices); i++ {
		d := devices[i]
		if isUSB(d.Devpath()) {
			short := filepath.Join("/dev", filepath.Base(d.Devpath()))
			log.Info("found %s", short)
			log.Divider()
			err := m.AddDevice(ctx, d)
			if err != nil {
				log.Error("%s : %s", short, err.Error())
			} else {
				log.Info("%s OK", short)
			}
			log.Divider()
		}
	}
}

func isUSB(name string) bool {
	name = filepath.Base(name)
	return strings.Contains(name, "ttyUSB")
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
	candidate, err := db.GetSymlinkCandidate(m.db, modem.IMEI)
	if err != nil {
		if db.DongleExists(m.db, modem) {
			log.Info("this dongle already exists")
			return nil
		}
	} else if candidate.TTY < modem.TTY {
		log.Info("a better candidate already exist at %s ", candidate.Path)
		return nil
	}
	log.Info("found dongle imei:%s imsi:%s path:%s",
		modem.IMEI, modem.IMSI, modem.Path,
	)
	m.stream.Send(e)
	if modem.IMSI == "" {
		log.Info("skipping processing dongle without imsi")
		return nil
	}
	return m.createAdnSym(modem)
}

// creates a dongle and symlinks it
func (m *Manager) createAdnSym(modem *db.Dongle) error {
	err := db.CreateDongle(m.db, modem)
	if err != nil {
		return err
	}
	return m.Symlink(modem)
}

func (m *Manager) symlink(d *db.Dongle) {
	n := fmt.Sprintf("/dev/%s.imei", d.IMEI)
	_ = syscall.Unlink(n)
	err := os.Symlink(d.Path, n)
	if err != nil {
		log.Error("symlink :  %v", err)
		return
	}
	log.Info("symlink: %s --> %s", n, d.Path)
	i := fmt.Sprintf("/dev/%s.imsi", d.IMSI)
	_ = syscall.Unlink(i)
	err = os.Symlink(d.Path, i)
	if err != nil {
		log.Error("ymlinks :  %v", err)
		_ = syscall.Unlink(n)
		return
	}
	d.IsSymlinked = true
	err = db.UpdateDongle(m.db, d)
	if err != nil {
		log.Error(err.Error())
	} else {
		e := &events.Event{Name: "update", Data: d}
		m.stream.Send(e)
	}
	log.Info("symlink: %s --> %s", i, d.Path)
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

//ClearSymlinks remove all symlinks that wrere created by this aoolication
func ClearSymlinks() error {
	return filepath.Walk("/dev", clearSymlink)

}

func clearSymlink(path string, info os.FileInfo, err error) error {
	if err != nil {
		return err
	}
	if info.IsDir() {
		return nil
	}
	e := filepath.Ext(path)
	if e == ".imei" || e == ".imsi" {
		log.Info("unlink: %s", path)
		return syscall.Unlink(path)
	}
	return nil
}

// Symlink creates symlink for the dongle. The symlinks are for both imei and imsi.
func (m *Manager) Symlink(d *db.Dongle) error {
	if d.IMSI == "" {
		return nil
	}
	c, err := db.GetSymlinkCandidate(m.db, d.IMEI)
	if err != nil {
		return err
	}
	if !c.IsSymlinked {
		m.symlink(c)
	}
	return nil
}

//FindModem thic=s checks if the udev Device is a donge. We are only interested
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
	log.Info("looking for modem at %s", name)
	start := time.Now()
	cfg := serial.Config{Name: name, Baud: 9600, ReadTimeout: 5 * time.Second}
	modem, err := NewModem(ctx, cfg)
	if err != nil {
		return nil, err
	}
	end := time.Now()
	log.Info("found it in %s", end.Sub(start).String())
	return modem, nil
}

func getttyNum(tty string) (int, error) {
	b := filepath.Base(tty)
	b = strings.TrimPrefix(b, "ttyUSB")
	return strconv.Atoi(b)
}

// NewModem talks to the device to determine if the device is a dongle
func NewModem(ctx context.Context, cfg serial.Config) (*db.Dongle, error) {
	m := &db.Dongle{}
	imsi, err := findIMSI(cfg, MaxAttempt)
	if err != nil {
		log.Error(err.Error())
	}
	imei, ati, err := findIMEI(cfg, MaxAttempt)
	if err != nil {
		return nil, err
	}
	m.IMEI = imei
	m.ATI = ati
	m.IMSI = imsi
	m.Path = cfg.Name
	i, err := getttyNum(m.Path)
	if err != nil {
		return nil, err
	}
	m.TTY = i
	return m, nil
}

func getIMEI(cfg serial.Config) (string, string, error) {
	c := &Conn{device: cfg}
	err := c.Open()
	if err != nil {
		return "", "", err
	}
	defer c.Close()
	o, err := c.Run("ATI")
	if err != nil {
		return "", "", err
	}
	ati := string(o)
	im, ok := getIMEINumber(ati)
	if !ok {
		return "", "", errors.New("IMEI not found")
	}
	return im, ati, nil
}

func getIMEINumber(src string) (string, bool) {
	src = strings.TrimSpace(src)
	if src == "" {
		return src, false
	}
	im := "IMEI:"
	gap := "+GCAP"
	i := strings.Index(src, im)
	if i == -1 {
		return "", false
	}
	g := strings.Index(src, gap)
	if i == -1 {
		return "", false
	}
	n := strings.TrimSpace(src[i+len(im) : g])
	return n, isNumber(n)
}

func findIMSI(cfg serial.Config, try int) (imsi string, err error) {
	var count int
	for count <= try {
		log.Info("trying to find imsi %d attempt", count)
		imsi, err = getIMSI(cfg)
		if err != nil {
			count++
			log.Info(err.Error())
			continue
		}
		log.Info("OK")
		return
	}
	return
}

func findIMEI(cfg serial.Config, try int) (imei, ati string, err error) {
	var count int
	for count <= try {
		log.Info("trying to find imei %d attempt", count)
		imei, ati, err = getIMEI(cfg)
		if err != nil {
			count++
			log.Info(err.Error())
			continue
		}
		log.Info("OK")
		return
	}
	return
}

func getIMSI(cfg serial.Config) (string, error) {
	c := &Conn{device: cfg}
	err := c.Open()
	if err != nil {
		return "", err
	}
	defer c.Close()
	o, err := c.Run("AT+CIMI")
	if err != nil {
		return "", err
	}
	im, ok := getIMSINumber(o)
	if !ok {
		return "", errors.New("IMSI not found")
	}
	return im, nil
}

func getIMSINumber(src []byte) (string, bool) {
	i, err := cleanResult(src)
	if err != nil {
		return "", false
	}
	im := string(i)
	if !isNumber(im) {
		return "", false
	}
	return im, true
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
	err := c.Flush()
	if err != nil {
		return nil, err
	}
	defer c.Flush()
	_, err = c.Write([]byte(cmd))
	if err != nil {
		return nil, err
	}
	/*
		buf := make([]byte, 128)
		_, err = c.Read(buf)
		if err != nil {
			return nil, err
		}
	*/
	buf, err := ioutil.ReadAll(c)
	if err != nil {
		return nil, err
	}
	buf = bytes.TrimSpace(buf)
	//fmt.Printf("CMD %s: TTY: %s ==> %s\n", cmd, c.device.Name, string(buf))
	if !bytes.Contains(buf, []byte("OK")) {
		return nil, errors.New(string(buf))
	}
	return buf, nil
}

// Run helper for Exec that adds \r to the command
func (c *Conn) Run(cmd string) ([]byte, error) {
	return c.Exec(fmt.Sprintf("%s\r\n", cmd))
}

func (c *Conn) Flush() error {
	if c.isOpen {
		return c.port.Flush()
	}
	return errors.New("can'f flaush a closed port")
}
