package tools

import "errors"

func makeFIFO(string) error {
	return errors.New("no FIFOs on Windows")
}
