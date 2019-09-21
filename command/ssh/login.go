package ssh

import (
	"time"

	"github.com/smallstep/cli/crypto/sshutil"

	"github.com/pkg/errors"
	"github.com/smallstep/certificates/api"
	"github.com/smallstep/certificates/authority/provisioner"
	"github.com/smallstep/cli/command"
	"github.com/smallstep/cli/crypto/keys"
	"github.com/smallstep/cli/errs"
	"github.com/smallstep/cli/flags"
	"github.com/smallstep/cli/ui"
	"github.com/smallstep/cli/utils/cautils"
	"github.com/urfave/cli"
	"golang.org/x/crypto/ssh"
)

func loginCommand() cli.Command {
	return cli.Command{
		Name:      "login",
		Action:    command.ActionFunc(loginAction),
		Usage:     "adds a SSH certificate into the authentication agent",
		UsageText: `**step ssh login** <principal>`,
		Description: `**step ssh login** command ...

## POSITIONAL ARGUMENTS

TODO

## EXAMPLES

TODO`,
		Flags: []cli.Flag{
			flags.Token,
			sshAddUserFlag,
			sshPasswordFileFlag,
			sshConnectFlag,
			flags.Provisioner,
			flags.NotBefore,
			flags.NotAfter,
			flags.CaURL,
			flags.Root,
			flags.Offline,
			flags.CaConfig,
			flags.Force,
		},
	}
}

func loginAction(ctx *cli.Context) error {
	if err := errs.NumberOfArguments(ctx, 1); err != nil {
		return err
	}

	// Arguments
	subject := ctx.Args().First()
	user := provisioner.SanitizeSSHUserPrincipal(subject)
	principals := []string{user}

	// Flags
	token := ctx.String("token")
	address := ctx.String("connect")
	isAddUser := ctx.Bool("add-user")
	force := ctx.Bool("force")
	validAfter, validBefore, err := flags.ParseTimeDuration(ctx)
	if err != nil {
		return err
	}

	// Connect with SSH agent if available
	agent, agentErr := sshutil.DialAgent()

	// Connect to the remote shell using the previous certificate in the agent
	if agent != nil && address != "" && !force {
		if signer, err := agent.GetSigner(subject); err == nil {
			shell, err := sshutil.NewShell(user, address, sshutil.WithSigner(signer))
			if err != nil {
				return err
			}
			return shell.RemoteShell()
		}
	}

	// Do step-certificates flow
	flow, err := cautils.NewCertificateFlow(ctx)
	if err != nil {
		return err
	}
	if len(token) == 0 {
		// Make sure the validAfter is in the past. It avoids `Certificate
		// invalid: not yet valid` errors if the times are not in sync
		// perfectly.
		if validAfter.IsZero() {
			validAfter = provisioner.NewTimeDuration(time.Now().Add(-1 * time.Minute))
		}

		if token, err = flow.GenerateSSHToken(ctx, subject, provisioner.SSHUserCert, principals, validAfter, validBefore); err != nil {
			return err
		}
	}

	caClient, err := flow.GetClient(ctx, subject, token)
	if err != nil {
		return err
	}

	// Generate keypair
	pub, priv, err := keys.GenerateDefaultKeyPair()
	if err != nil {
		return err
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return errors.Wrap(err, "error creating public key")
	}

	var sshAuPub ssh.PublicKey
	var sshAuPubBytes []byte
	var auPub, auPriv interface{}
	if isAddUser {
		auPub, auPriv, err = keys.GenerateDefaultKeyPair()
		if err != nil {
			return err
		}
		sshAuPub, err = ssh.NewPublicKey(auPub)
		if err != nil {
			return errors.Wrap(err, "error creating public key")
		}
		sshAuPubBytes = sshAuPub.Marshal()
	}

	resp, err := caClient.SignSSH(&api.SignSSHRequest{
		PublicKey:        sshPub.Marshal(),
		OTT:              token,
		Principals:       principals,
		CertType:         provisioner.SSHUserCert,
		ValidAfter:       validAfter,
		ValidBefore:      validBefore,
		AddUserPublicKey: sshAuPubBytes,
	})
	if err != nil {
		return err
	}

	if agent == nil {
		ui.Printf(`{{ "%s" | red }} {{ "SSH Agent:" | bold }} %v`+"\n", ui.IconBad, agentErr)
	} else {
		// Attempt to add key to agent if private key defined.
		if err := agent.AddCertificate(subject, resp.Certificate.Certificate, priv); err != nil {
			ui.Printf(`{{ "%s" | red }} {{ "SSH Agent:" | bold }} %v`+"\n", ui.IconBad, err)
		} else {
			ui.PrintSelected("SSH Agent", "yes")
		}
		if isAddUser {
			if err := agent.AddCertificate(subject, resp.AddUserCertificate.Certificate, auPriv); err != nil {
				ui.Printf(`{{ "%s" | red }} {{ "SSH Agent:" | bold }} %v`+"\n", ui.IconBad, err)
			} else {
				ui.PrintSelected("SSH Agent", "yes")
			}
		}
	}

	// Connect to the remote shell using the new certificate
	if address != "" {
		shell, err := sshutil.NewShell(user, address, sshutil.WithCertificate(resp.Certificate.Certificate, priv))
		if err != nil {
			return err
		}
		return shell.RemoteShell()
	}

	return nil
}