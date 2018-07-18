package kdf

import (
	"crypto/subtle"
	"fmt"

	"github.com/pkg/errors"
	"github.com/smallstep/cli/command/crypto/internal/utils"
	"github.com/smallstep/cli/errs"
	"github.com/urfave/cli"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/scrypt"
)

// Command returns the cli.Command for kdf and related subcommands.
func Command() cli.Command {
	return cli.Command{
		Name:      "kdf",
		Usage:     "key derivation functions for password hashing and verification",
		UsageText: "step crypto kdf <SUBCOMMAND> [SUBCOMMAND_FLAGS]",
		Subcommands: cli.Commands{
			hashCommand(),
			compareCommand(),
		},
	}
}

func hashCommand() cli.Command {
	return cli.Command{
		Name:      "hash",
		Action:    cli.ActionFunc(hashAction),
		Usage:     "derive a secret key from a secret value (e.g., a password)",
		UsageText: "step crypto kdf hash [INPUT] [--alg ALGORITHM]",
		Description: `The 'step crypto kdf hash' command uses a key derivation function (KDF) to
produce a pseudorandom secret key based on some (presumably secret) input
value. This is useful for password verification approaches based on password
hashing. Key derivation functions are designed to be computationally
intensive, making it more difficult for attackers to perform brute-force
attacks on password databases.

  If this command is run without the optional INPUT argument and STDIN is a
TTY (i.e., you're running the command in an interactive terminal and not
piping input to it) you'll be prompted to enter a value on STDERR. If STDIN is
not a TTY it will be read without prompting.

  This command will produce a string encoding of the KDF output along with the
algorithm used, salt, and any parameters required for validation in PHC string
format.

  The KDFs are run with parameters that are considered safe. The 'scrypt'
parameters are currently fixed at N=32768, r=8 and p=1. The 'bcrypt' work
factor is currently fixed at 10.

POSITIONAL ARGUMENTS

  INPUT
    The input to the key derivation function. INPUT is optional and its use is
not recommended. If this argument is provided the '--insecure' flag must also
be provided because your (presumably secret) INPUT will likely be logged and
appear in places you might not expect. If omitted input is read from STDIN.
		`,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "alg",
				Value: "scrypt",
				Usage: `The KDF algorithm to use.

  ALGORITHM must be one of:
    scrypt
      A password-based KDF designed to use exponential time and memory.
    bcrypt
      A password-based KDF designed to use exponential time.`,
			},
			cli.BoolFlag{
				Name:   "insecure",
				Hidden: true,
			},
		},
	}
}

func hashAction(ctx *cli.Context) error {
	var err error
	var input []byte

	// Get kdf method
	var kdf func([]byte) (string, error)
	switch alg := ctx.String("alg"); alg {
	case "scrypt":
		kdf = doScrypt
	case "bcrypt":
		kdf = doBcrypt
	default:
		return errs.InvalidFlagValue(ctx, "alg", alg, "")
	}

	// Grab input from terminal or arguments
	switch ctx.NArg() {
	case 0:
		input, err = utils.ReadInput("Enter password to hash: ")
		if err != nil {
			return err
		}
	case 1:
		if !ctx.Bool("insecure") {
			return errs.InsecureArgument(ctx, "INPUT")
		}
		input = []byte(ctx.Args().Get(0))
	default:
		return errs.TooManyArguments(ctx)
	}

	// Hash input
	hash, err := kdf(input)
	if err != nil {
		return err
	}

	fmt.Println(hash)
	return nil
}

// doScrypt uses scrypt-32768 to derive the given password.
func doScrypt(password []byte) (string, error) {
	salt, err := phcGetSalt(16)
	if err != nil {
		return "", errors.Wrap(err, "error creating salt")
	}
	// use scrypt-32768 by default
	p := scryptParams[scryptHash32768]
	hash, err := scrypt.Key(password, salt, p.N, p.r, p.p, p.kl)
	if err != nil {
		return "", errors.Wrap(err, "error deriving password")
	}

	return phcEncode("scrypt", p.getParams(), salt, hash), nil
}

// doBcrypt uses bcrypt to derive the given password.
func doBcrypt(password []byte) (string, error) {
	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		return "", errors.Wrap(err, "error deriving password")

	}
	return string(hash), nil
}

func compareCommand() cli.Command {
	return cli.Command{
		Name:      "compare",
		Action:    cli.ActionFunc(compareAction),
		Usage:     "compare a plaintext value (e.g., a password) and a hash",
		UsageText: "step crypto kdf compare PHC_HASH [INPUT]",
		Description: `The 'step crypto kdf compare' command compares a plaintext value (e.g., a
password) with an existing KDF password hash in PHC string format. The PHC
string input indicates which KDF algorithm and parameters to use.

  If the input matches PHC_HASH the command prints a human readable message
indicating success to STDERR and returns 0. If the input does not match an
error will be printed to STDERR and the command will exit with a non-zero
return code.

  If this command is run without the optional INPUT argument and STDIN is a
TTY (i.e., you're running the command in an interactive terminal and not
piping input to it) you'll be prompted to enter a value on STDERR. If STDIN is
not a TTY it will be read without prompting.

POSITIONAL ARGUMENTS

  INPUT
    The plaintext value to compare with PHC_HASH. INPUT is optional and its
use is not recommended. If this argument is provided the '--insecure' flag
must also be provided because your (presumably secret) INPUT will likely be
logged and appear in places you might not expect. If omitted input is read
from STDIN.`,
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:   "insecure",
				Hidden: true,
			},
		},
	}
}

func compareAction(ctx *cli.Context) error {
	var err error
	var hashStr string
	var input []byte

	switch ctx.NArg() {
	case 0:
		return errs.MissingArguments(ctx, "PHC_HASH")
	case 1:
		hashStr = ctx.Args().Get(0)
		input, err = utils.ReadInput("Enter password to compare: ")
		if err != nil {
			return err
		}
	case 2:
		if !ctx.Bool("insecure") {
			return errs.InsecureArgument(ctx, "INPUT")
		}
		args := ctx.Args()
		hashStr, input = args[0], []byte(args[1])
	default:
		return errs.TooManyArguments(ctx)
	}

	id, params, salt, hash, err := phcDecode(hashStr)
	if err != nil {
		return errors.Wrap(err, "error decoding hash")
	}

	var valid bool
	switch id {
	case bcryptHash:
		valid = (bcrypt.CompareHashAndPassword(hash, input) == nil)
	case scryptHash:
		p, err := newScryptParams(params)
		if err != nil {
			return err
		}
		hashedPass, err := scrypt.Key(input, salt, p.N, p.r, p.p, len(hash))
		if err != nil {
			return errors.Wrap(err, "error deriving input")
		}
		valid = (subtle.ConstantTimeCompare(hash, hashedPass) == 1)
	default:
		return errors.Errorf("invalid or unsupported hash method with id '%s'", id)
	}

	if valid {
		fmt.Println("ok")
		return nil
	}

	return errors.New("fail")
}