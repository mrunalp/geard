package cleanup

import (
	"bufio"
	"os"
	"os/exec"
	"os/user"
	"strings"

	"github.com/openshift/geard/containers"
)

type UsersCleanup struct {
}

func init() {
	AddCleaner(&UsersCleanup{})
}

// Removes container users that aren't used by any containers.
func (r *UsersCleanup) Clean(ctx *CleanerContext) {
	ctx.LogInfo.Println("--- HOST USERS CLEANUP ---")

	f, err := os.Open("/etc/passwd")
	if err != nil {
		ctx.LogError.Println("Failed to open /etc/passwd: %v", err)
		return
	}
	defer f.Close()

	ctrUsers := make(map[string]string)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		parts := strings.Split(line, ":")
		user := parts[0]
		if strings.HasPrefix(user, containers.IdentifierPrefix) {
			ctrUsers[user] = line
		}
	}

	for userName, _ := range ctrUsers {
		hostUser, err := user.Lookup(userName)
		if err != nil {
			ctx.LogError.Printf("Unable to lookup user: %v", err)
			continue
		}

		id, err := containers.NewIdentifierFromUser(hostUser)
		if err != nil {
			ctx.LogError.Printf("Unable to create identifier from username: %v", err)
			continue
		}

		// Check if a unit file exists corresponding to the user. If not,
		// then remove the user.
		unitFilePath := id.UnitPathFor()
		if _, err := os.Stat(unitFilePath); os.IsNotExist(err) {
			if ctx.DryRun {
				ctx.LogInfo.Printf("%v could be removed.", userName)
			} else {
				ctx.LogInfo.Printf("Removing user %v.", userName)
				cmd := exec.Command("/usr/sbin/userdel", userName)
				if out, err := cmd.CombinedOutput(); err != nil {
					ctx.LogError.Printf("Failed to remove user: %v %v", out, err)
				}
			}
		}
	}
}
