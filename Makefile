HOSTNAME    = registry.terraform.io
NAMESPACE   = davispalomino
NAME        = vaultpatch
BINARY      = terraform-provider-${NAME}
VERSION     = 0.1.0
OS_ARCH     = darwin_arm64
INSTALL_DIR = ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}

GOROOT      = /usr/local/Cellar/go/1.21.6/libexec
GO          = GOROOT=${GOROOT} GOTOOLCHAIN=local GOOS=darwin GOARCH=arm64 ${GOROOT}/bin/go

default: build

build:
	${GO} build -o ${BINARY}

install: build
	mkdir -p ${INSTALL_DIR}
	cp ${BINARY} ${INSTALL_DIR}/

clean:
	rm -f ${BINARY}
	rm -rf ${INSTALL_DIR}

test:
	${GO} test ./... -v

.PHONY: build install clean test
