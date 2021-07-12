package main

import (
	"context"
	"log"
	"net"
	"os"
	"strconv"

	"github.com/zeebo/errs/v2"
)

func closeListener(lis net.Listener) {
	if lis != nil {
		_ = lis.Close()
	}
}

func listen(ctx context.Context) (lis443, lis80 net.Listener, err error) {
	defer func() {
		if err != nil {
			closeListener(lis443)
			closeListener(lis80)
		}
	}()

	lis443, lis80, err = listenFromEnv(ctx)
	if err != nil {
		log.Printf("error listening from env: %+v", err)
	}

	if lis443 == nil {
		lis443, err = net.Listen("tcp", ":443")
		if err != nil {
			return lis443, lis80, errs.Wrap(err)
		}
	}

	if lis80 == nil {
		lis80, err = net.Listen("tcp", ":80")
		if err != nil {
			return lis443, lis80, errs.Wrap(err)
		}
	}

	return lis443, lis80, nil
}

func listenFromEnv(ctx context.Context) (lis443, lis80 net.Listener, err error) {
	defer func() {
		if err != nil {
			closeListener(lis443)
			closeListener(lis80)
		}
	}()

	envPid, envFDs := os.Getenv("LISTEN_PID"), os.Getenv("LISTEN_FDS")
	if envPid == "" || envFDs == "" {
		return nil, nil, nil
	}
	pid, err := strconv.Atoi(envPid)
	if err != nil {
		return lis443, lis80, errs.Wrap(err)
	} else if pid != os.Getpid() {
		return lis443, lis80, errs.Errorf("invalid pid specified")
	}
	nfds, err := strconv.Atoi(envFDs)
	if err != nil {
		return lis443, lis80, errs.Wrap(err)
	}

	for i := 0; i < nfds; i++ {
		lis, err := net.FileListener(os.NewFile(uintptr(3+i), ""))
		if err != nil {
			return lis443, lis80, errs.Wrap(err)
		}
		if lis.Addr().Network() != "tcp" {
			_ = lis.Close()
			continue
		}
		switch addr := lis.Addr().String(); {
		case lis443 == nil && addr == "0.0.0.0:443":
			lis443 = lis
		case lis80 == nil && addr == "0.0.0.0:80":
			lis80 = lis
		default:
			_ = lis.Close()
		}
	}

	return lis443, lis80, nil
}
