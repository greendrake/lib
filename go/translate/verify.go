package translate

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// verify runs the host's own consumers over what was just written. They are
// the processes that would otherwise discover a defect — a service at boot, a
// frontend build — so a run that satisfies them is a run that cannot wedge
// either. What they are is the host's business; the engine only runs them.
func (m *Manifest) verify(log func(string)) error {
	for _, command := range m.Verify {
		cmd := exec.Command(command.Run[0], command.Run[1:]...)
		cmd.Dir = m.path(command.Cwd)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("verify `%s` refused what was just written: %w", strings.Join(command.Run, " "), err)
		}
		log(fmt.Sprintf("verified by `%s`", strings.Join(command.Run, " ")))
	}
	return nil
}
