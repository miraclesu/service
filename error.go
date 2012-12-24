package service

import (
    "errors"
    "fmt"
)

var (
    ErrNoActiveServer = errors.New("No active server.")
    ErrServiceWarning = errors.New("The Service does not running.")
)

func Errorf(format string, msg ...interface{}) error {
    return errors.New(fmt.Sprintf(format, msg...))
}

type ErrorHandler func(e error)
type MessageHandler func(e error)
