package mister

import "syscall"

func createFIFO(path string) error {
	return syscall.Mkfifo(path, 0666)
}
