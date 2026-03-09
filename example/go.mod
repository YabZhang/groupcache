module example

go 1.22

replace github.com/golang/groupcache => ../

require github.com/golang/groupcache v0.0.0-00010101000000-000000000000

require (
	github.com/golang/protobuf v1.5.4 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
)
