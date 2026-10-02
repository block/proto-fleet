//go:build unix

package main

import "syscall"

const webNonblock = syscall.O_NONBLOCK
