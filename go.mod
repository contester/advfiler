module github.com/contester/advfiler

go 1.19

replace stingr.net/go/efstore => ../efstore

require (
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/prometheus/client_golang v1.15.1
	github.com/sirupsen/logrus v1.9.0
	golang.org/x/net v0.9.0
	google.golang.org/protobuf v1.30.0
	stingr.net/go/systemdutil v0.0.0-20210311175859-735e4cc44e94
)

require golang.org/x/exp v0.0.0-20230510235704-dd950f8aeaea // indirect

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/coreos/go-systemd/v22 v22.4.0 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/prometheus/client_model v0.4.0 // indirect
	github.com/prometheus/common v0.43.0 // indirect
	github.com/prometheus/procfs v0.9.0 // indirect
	github.com/syndtr/goleveldb v1.0.0
	golang.org/x/sys v0.8.0 // indirect
	golang.org/x/text v0.9.0 // indirect
	stingr.net/go/efstore v0.0.0-20221028185138-636fdf42bef5
)
