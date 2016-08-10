package main

import (
  "github.com/mitchellh/packer/packer/plugin"
  "github.com/anthcourtney/packer-post-processor-ami-volume-permissions/permissions"
)

func main() {
  server, err := plugin.Server()
  if err != nil {
    panic(err)
  }
  server.RegisterPostProcessor(new(permissions.PostProcessor))
  server.Serve()
}
