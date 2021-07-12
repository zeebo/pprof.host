pprof.host: *.go go.mod go.sum
	go build
	sudo setcap CAP_NET_BIND_SERVICE=+eip ./pprof.host
