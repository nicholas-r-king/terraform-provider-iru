// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/provider"
)

var version = "dev"

func main() {
	debug := flag.Bool("debug", false, "enable debugger attachment")
	flag.Parse()
	if err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/nicholas-r-king/iru",
		Debug:   *debug,
	}); err != nil {
		log.Fatal(err)
	}
}
