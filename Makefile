VERSION=0.1.4
NAME=fdevices_$(VERSION)

build:
	gox  \
		-output "bin/{{.Dir}}/{{.OS}}_{{.Arch}}/{{.Dir}}_$(VERSION)/{{.Dir}}" \
		-osarch "linux/arm" github.com/FarmRadioHangar/fdevices

tar:
	cd bin/ && tar -zcvf $(NAME).tar.gz  devices/
