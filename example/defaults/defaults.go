package main

import (
	"fmt"

	"github.com/FranciscoPaixao/opts"
)

type Config struct {
	Foo string
	Bar string
}

func main() {

	c := Config{
		Bar: "moon",
	}

	opts.Parse(&c)

	fmt.Println(c.Foo)
	fmt.Println(c.Bar)
}
