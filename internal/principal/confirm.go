package principal

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const ttyPath = "/dev/tty"

// Confirm prints question to tty, reads one line from it, and returns an error
// unless the trimmed line equals want.
func Confirm(tty io.ReadWriter, question, want string) error {
	if _, err := fmt.Fprint(tty, question); err != nil {
		return err
	}
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return fmt.Errorf("no answer read: %w", err)
	}
	if strings.TrimSpace(line) != want {
		return fmt.Errorf("answer did not match %q; nothing recorded", want)
	}
	return nil
}

// OpenTTY opens the controlling terminal. It fails when there is none.
func OpenTTY() (*os.File, error) {
	return os.OpenFile(ttyPath, os.O_RDWR, 0)
}
