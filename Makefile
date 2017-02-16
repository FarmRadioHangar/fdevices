VERSION=0.1.5
NAME=fdevices_$(VERSION)

build:
	gox  \
		-output "bin/{{.Dir}}_$(VERSION)/{{.OS}}_{{.Arch}}/{{.Dir}}" \
		-osarch "linux/arm" github.com/FarmRadioHangar/fdevices

tar: prep
	cd bin/ && tar -zcvf $(NAME).tar.gz  fdevices_$(VERSION)/

prep:
	cp fdevices.service bin/fdevices_$(VERSION)/
