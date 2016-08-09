package main

import (
  "github.com/anthcourtney/packer-post-processor-ami-volume-permissions/ami-volume-permissions"
  "github.com/mitchellh/packer/packer/plugin"
)

func main() {
	server, err := plugin.Server()
	if err != nil {
		panic(err)
	}
	server.RegisterPostProcessor(new(ami-volume-permissions.PostProcessor{})
	server.Serve()
}
