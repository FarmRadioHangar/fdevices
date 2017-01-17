VERSION=0.1.2
NAME=devices_$(VERSION)
OUT_DIR=bin/linux_arm/devices_$(VERSION)

all:$(OUT_DIR)/devices
$(OUT_DIR)/devices:main.go
	gox  \
		-output "bin/{{.Dir}}/{{.OS}}_{{.Arch}}/{{.Dir}}_$(VERSION)/{{.Dir}}" \
		-osarch "linux/arm" github.com/FarmRadioHangar/devices

tar:
	cd bin/ && tar -zcvf devices_$(VERSION).tar.gz  devices/
