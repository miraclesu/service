package service

import (
    "errors"
    "fmt"
)

var (
    ErrNoActiveServer = errors.New("No active server.")
)

func Errorf(format string, msg ...interface{}) error {
    return errors.New(fmt.Sprintf(format, msg...))
}

type ErrorHandler func(e error)
