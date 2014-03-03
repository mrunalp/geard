package jobs

import (
	"code.google.com/p/go.crypto/ssh"
	"crypto/sha256"
	"errors"
	"github.com/smarterclayton/geard/gears"
	"github.com/smarterclayton/geard/utils"
	"log"
	"os"
)

type CreateKeysRequest struct {
	UserId string
	Data   *ExtendedCreateKeysData
}

type ExtendedCreateKeysData struct {
	Keys         []KeyData
	Repositories []RepositoryPermission
	Gears        []GearPermission
}

type KeyData struct {
	Type  string
	Value string
}

type RepositoryPermission struct {
	Id    gears.Identifier
	Write bool
}

type GearPermission struct {
	Id gears.Identifier
}

func (k *KeyData) Check() error {
	switch k.Type {
	case "ssh-rsa", "ssh-dsa", "ssh-ecdsa":
	default:
		return errors.New("Type must be one of 'ssh-rsa', 'ssh-dsa', or 'ssh-ecdsa'")
	}
	if k.Value == "" {
		return errors.New("Value must be specified.")
	}
	return nil
}

func (p *RepositoryPermission) Check() error {
	_, err := gears.NewIdentifier(string(p.Id))
	return err
}

func (p *GearPermission) Check() error {
	_, err := gears.NewIdentifier(string(p.Id))
	return err
}

func (d *ExtendedCreateKeysData) Check() error {
	for i := range d.Keys {
		if err := d.Keys[i].Check(); err != nil {
			return err
		}
	}
	for i := range d.Gears {
		if err := d.Gears[i].Check(); err != nil {
			return err
		}
	}
	for i := range d.Repositories {
		if err := d.Repositories[i].Check(); err != nil {
			return err
		}
	}
	if len(d.Keys) == 0 {
		return errors.New("One or more keys must be specified.")
	}
	if len(d.Repositories) == 0 && len(d.Gears) == 0 {
		return errors.New("Either repositories or gears must be specified.")
	}
	return nil
}

type KeyFailure struct {
	Index  int
	Key    *KeyData
	Reason error
}

type KeyStructuredFailure struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

func KeyFingerprint(key ssh.PublicKey) utils.Fingerprint {
	bytes := sha256.Sum256(key.Marshal())
	return utils.Fingerprint(bytes[:])
}

func (j *CreateKeysRequest) Execute(resp JobResponse) {
	failedKeys := []KeyFailure{}
	for i := range j.Data.Keys {
		key := j.Data.Keys[i]
		pk, _, _, _, ok := ssh.ParseAuthorizedKey([]byte(key.Value))
		if !ok {
			failedKeys = append(failedKeys, KeyFailure{i, &key, errors.New("Unable to parse key")})
			continue
		}

		value := ssh.MarshalAuthorizedKey(pk)
		fingerprint := KeyFingerprint(pk)
		path := fingerprint.PublicKeyPathFor()

		if err := utils.AtomicWriteToContentPath(path, 0660, value); err != nil {
			failedKeys = append(failedKeys, KeyFailure{i, &key, err})
			continue
		}

		for k := range j.Data.Gears {
			p := j.Data.Gears[k]
			if _, err := os.Stat(p.Id.UnitPathFor()); err != nil {
				failedKeys = append(failedKeys, KeyFailure{i, &key, err})
			}
			if err := os.Symlink(path, p.Id.SshAccessPathFor(fingerprint)); err != nil && !os.IsExist(err) {
				failedKeys = append(failedKeys, KeyFailure{i, &key, err})
			}
			if _, err := os.Stat(p.Id.AuthKeysPathFor()); err == nil {
				if err := os.Remove(p.Id.AuthKeysPathFor()); err != nil {
					failedKeys = append(failedKeys, KeyFailure{i, &key, err})
				}
			}
		}
		for k := range j.Data.Repositories {
			p := j.Data.Repositories[k]
			if _, err := os.Stat(p.Id.RepositoryPathFor()); err != nil {
				failedKeys = append(failedKeys, KeyFailure{i, &key, err})
			}
			accessPath := p.Id.GitAccessPathFor(fingerprint, p.Write)
			if err := os.Symlink(path, accessPath); err != nil && !os.IsExist(err) {
				failedKeys = append(failedKeys, KeyFailure{i, &key, err})
			}
		}
	}

	if len(failedKeys) > 0 {
		data := make([]KeyStructuredFailure, len(failedKeys))
		for i := range failedKeys {
			data[i] = KeyStructuredFailure{failedKeys[i].Index, failedKeys[i].Reason.Error()}
			log.Printf("Failure %d: %+v", failedKeys[i].Index, failedKeys[i].Reason)
		}
		resp.Failure(StructuredJobError{SimpleJobError{JobResponseError, "Not all keys were completed"}, data})
	} else {
		resp.Success(JobResponseOk)
	}
}
