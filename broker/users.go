package broker

import (
	"context"

	"github.com/cloudway/platform/auth/userdb"
	"github.com/cloudway/platform/pkg/errors"
)

func (br *Broker) CreateUser(user userdb.User, password string) (err error) {
	basic := user.Basic()

	// create the user in the database
	err = br.Users.Create(user, password)
	if err != nil {
		return err
	}

	// create the namespace in the SCM
	if basic.Namespace != "" {
		err = br.SCM.CreateNamespace(basic.Namespace)
		if err != nil {
			br.Users.Remove(basic.Name)
			return err
		}
	}

	return nil
}

func (br *Broker) RemoveUser(username string) (err error) {
	ctx := context.Background()

	var user userdb.BasicUser
	err = br.Users.Find(username, &user)
	if err != nil {
		return err
	}

	var errors errors.Errors

	if user.Namespace != "" {
		// remove all containers belongs to the user
		cs, err := br.FindInNamespace(ctx, user.Namespace)
		if err != nil {
			errors.Add(err)
		} else {
			for _, c := range cs {
				errors.Add(c.Destroy(ctx))
			}
		}

		// remove the namespace from SCM
		errors.Add(br.SCM.RemoveNamespace(user.Namespace))

		// remove the namespace from the plugin hub
		br.Hub.RemoveNamespace(user.Namespace)
	}

	// remove user from user database
	errors.Add(br.Users.Remove(user.Name))

	return errors.Err()
}

func (br *Broker) GetUser(username string) (userdb.User, error) {
	var user userdb.BasicUser
	err := br.Users.Find(username, &user)
	return &user, err
}
