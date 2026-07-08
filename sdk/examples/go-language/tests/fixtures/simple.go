package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("Hello, World!")
}

func helper(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

type Config struct {
	Port    int
	Host    string
	Debug   bool
}

func NewConfig() *Config {
	return &Config{Port: 8080, Host: "localhost"}
}
