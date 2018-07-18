package jwt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"time"

	"github.com/pkg/errors"
	"github.com/smallstep/cli/command/crypto/internal/jose"
	"github.com/smallstep/cli/command/crypto/internal/utils"
	"github.com/smallstep/cli/errs"
	"github.com/urfave/cli"
)

func signCommand() cli.Command {
	return cli.Command{
		Name:   "sign",
		Action: cli.ActionFunc(signAction),
		Usage:  "create a signed JWT data structure",
		UsageText: `step crypto jwt sign [- | FILENAME] [--alg ALGORITHM] [--aud AUDIENCE] [--iss ISSUER] [--sub SUB]
        [--exp EXPIRATION] [--iat ISSUED_AT] [--nbf NOT_BEFORE] [--key JWK]
        [--jwks JWKS] [--kid KID] [--jti JTI]`,
		Description: `The 'step crypto jwt sign' command generates a signed JSON Web Token (JWT)
by computing a digital signature or message authentication code for a JSON
payload. By default, the payload to sign is read from STDIN and the JWT will
be written to STDOUT. The suggested pronunciation of JWT is the same as the
English word "jot".

  A JWT is a compact data structure used to represent some JSON encoded
"claims" that are passed as the payload of a JWS or JWE structure, enabling
the claims to be digitally signed and/or encrypted. The "claims" (or "claim
set") are represented as an ordinary JSON object. JWTs are represented using a
compact format that's URL safe and can be used in space-constrained
environments. JWTs can be passed in HTTP Authorization headers and as URI
query parameters.

  A "claim" is a piece of information asserted about a subject, represented as
a key/value pair. Logically a verified JWT should be interpreted as "ISSUER
says to AUDIENCE that SUBJECT's CLAIM_NAME is CLAIM_VALUE" for each claim.

  Some optional arguments introduce subtle security considerations if omitted.
These considerations should be carefully analyzed. Therefore, omitting SUBTLE
arguments requires the use of the '--subtle' flag as a misuse prevention
mechanism.

  A JWT signed using JWS has three parts:
    1. A base64 encoded JSON object representing the JOSE (JSON Object Signing
       and Encryption) header that describes the cryptographic operations
       applied to the JWT Claims Set
    2. A base64 encoded JSON object representing the JWT Claims Set
    3. A base64 encoded digital signature of message authentication code`,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name: "alg, algorithm",
				Usage: `The signature or MAC algorithm to use. Algorithms are case-sensitive strings
defined in RFC7518. The selected algorithm must be compatible with the key
type. This flag is optional. If not specified, the "alg" member of the JWK is
used. If the JWK has no "alg" member then a default is selected depending on
the JWK key type. If the JWK has an "alg" member and the "alg" flag is passed
the two options must match unless the '--subtle' flag is also passed.

    ALGORITHM is a case-sensitive string and must be one of:

     HS256
      HMAC using SHA-256 (default for "oct" key type)
     HS384
      HMAC using SHA-384
     HS512
      HMAC using SHA-512
     RS256
      RSASSA-PKCS1-v1_5 using SHA-256 (default for "RSA" key type)
     RS384
      RSASSA-PKCS1-v1_5 using SHA-384
     RS512
      RSASSA-PKCS1-v1_5 using SHA-512
     ES256
      ECDSA using P-256 and SHA-256 (default for "EC" key type)
     ES384
      ECDSA using P-384 and SHA-384
     ES512
      ECDSA using P-521 and SHA-512
     PS256
      RSASSA-PSS using SHA-256 and MGF1 with SHA-256
     PS384
      RSASSA-PSS using SHA-384 and MGF1 with SHA-384
     PS512
      RSASSA-PSS using SHA-512 and MGF1 with SHA-512
     EdDSA
      EdDSA signature algorithm`,
			},
			cli.StringFlag{
				Name: "iss, issuer",
				Usage: `The issuer of this JWT. The processing of this claim is generally
application specific. Typically, the ISSUER must match the name of some
trusted entity (e.g., an identity provider like "https://accounts.google.com")
and identify which key(s) to use for JWT verification and/or decryption (e.g.,
the keys at "https://www.googleapis.com/oauth2/v3/certs"). ISSUER is a
case-sensitive string.`,
			},
			cli.StringSliceFlag{
				Name: "aud, audience",
				Usage: `The intended recipient(s) of the JWT, encoded as the "aud" claim in the JWT.
Recipient(s) must identify themselves with one or more of the values in the
"aud" claim. The "aud" claim can be a string (indicating a single recipient)
or an array (indicating multiple potential recipients). This flag can be used
multiple times to generate a JWK with multiple intended recipients. Each
AUDIENCE is a case-sensitive string.`,
			},
			cli.StringFlag{
				Name: "sub, subject",
				Usage: `The subject of this JWT. The "claims" are normally interpreted as statements
about this subject. The subject must either be locally unique in the context
of the issuer or globally unique. The processing of this claim is generally
application specific. SUBJECT is a case-sensitive string.`,
			},
			cli.Int64Flag{
				Name: "exp, expiration",
				Usage: `The expiration time on or after which the JWT must not be accepted. EXPIRATION
must be a numeric value representing a Unix timestamp.`,
			},
			cli.Int64Flag{
				Name: "nbf, not-before",
				Usage: `The time before which the JWT must not be accepted. NOT_BEFORE must be a
numeric value representing a Unix timestamp. If not provided, the current time
is used.`,
			},
			cli.Int64Flag{
				Name: "iat, issued-at",
				Usage: `The time at which the JWT was issued, used to determine the age of the JWT.
ISSUED_AT must be a numeric value representing a Unix timestamp. If not
provided, the current time is used.`,
			},
			cli.StringFlag{
				Name: "jti, jwt-id",
				Usage: `A unique identifier for the JWT. The identifier must be assigned in a manner
that ensures that there is a negligible probability that the same value will
be accidentally assigned to multiple JWTs. The JTI claim can be used to
prevent a JWT from being replayed (i.e., recipient(s) can use JTI to make a
JWT one-time-use). The JTI argument is a case-sensitive string. If the '--jti'
flag is used without an argument a JTI will be generated randomly with
sufficient entropy to satisfy the collision-resistance criteria.`,
			},
			cli.StringFlag{
				Name: "key",
				Usage: `The key to use to sign the JWT. The KEY argument should be the name of a file.
JWTs can be signed using a private JWK (or a JWK encrypted as a JWE payload)
or a PEM encoded private key (or a private key encrypted using [TODO: insert
private key encryption mechanism]).`,
			},
			cli.StringFlag{
				Name: "jwks",
				Usage: `The JWK Set containing the key to use to sign the JWT. The JWKS argument
should be the name of a file. The file contents should be a JWK Set or a JWE
with a JWK Set payload. The '--jwks' flag requires the use of the '--kid' flag
to specify which key to use.`,
			},
			cli.StringFlag{
				Name: "kid",
				Usage: `The ID of the key used to sign the JWT. The KID argument is a case-sensitive
string. When used with '--jwk' the KID value must match the
"kid" member of the JWK. When used with '--jwks' (a JWK Set) the KID value must
match the "kid" member of one of the JWKs in the JWK Set.`,
			},
			cli.BoolFlag{
				Name:   "subtle",
				Hidden: true,
			},
			cli.BoolFlag{
				Name:   "no-kid",
				Hidden: true,
			},
		},
	}
}

func signAction(ctx *cli.Context) error {
	var err error
	var payload interface{}

	// Read payload if provided
	args := ctx.Args()
	switch len(args) {
	case 0:
		// empty extra payload
		payload = make(map[string]interface{})
	case 1:
		// read payload from file or stdin (-)
		if payload, err = readPayload(args[0]); err != nil {
			return err
		}
	default:
		return errors.Errorf("unknown arguments %v", args[1:])
	}

	isSubtle := ctx.Bool("subtle")
	alg := ctx.String("alg")

	// Validate key, jwks and kid
	key := ctx.String("key")
	jwks := ctx.String("jwks")
	kid := ctx.String("kid")
	switch {
	case key == "" && jwks == "":
		return errs.RequiredOrFlag(ctx, "key", "jwks")
	case key != "" && jwks != "":
		return errs.MutuallyExclusiveFlags(ctx, "key", "jwks")
	case jwks != "" && kid == "":
		return errs.RequiredWithFlag(ctx, "kid", "jwks")
	}

	// Read key from --key or --jwks
	var jwk *jose.JSONWebKey
	switch {
	case key != "":
		jwk, err = jose.ParseKey(key, "sig", alg, kid, isSubtle)
	case jwks != "":
		jwk, err = jose.ParseKeySet(jwks, alg, kid, isSubtle)
	default:
		return errs.RequiredOrFlag(ctx, "key", "jwks")
	}
	if err != nil {
		return err
	}

	// Public keys cannot be used for signing
	if jwk.IsPublic() {
		return errors.New("cannot use a public key for signing")
	}

	// Key "use" must be "sig" to use for signing
	if jwk.Use != "sig" && jwk.Use != "" {
		return errors.Errorf("invalid jwk use: found '%s', expecting 'sig' (signature)", jwk.Use)
	}

	// At this moment jwk.Algorithm should have an alg from:
	//  * alg parameter
	//  * jwk or jwkset
	//  * guessed for ecdsa and Ed25519 keys
	if jwk.Algorithm == "" {
		return errors.New("flag '--alg' is required with the given key")
	}
	if err := jose.ValidateJWK(jwk); err != nil {
		return err
	}

	// Add claims
	c := &jose.Claims{
		Issuer:    ctx.String("iss"),
		Subject:   ctx.String("sub"),
		Audience:  ctx.StringSlice("aud"),
		Expiry:    jose.NumericDate(ctx.Int64("exp")),
		NotBefore: jose.NumericDate(ctx.Int64("nbf")),
		IssuedAt:  jose.NumericDate(ctx.Int64("iat")),
		ID:        ctx.String("jti"),
	}
	now := time.Now()
	if c.NotBefore == 0 {
		c.NotBefore = jose.NewNumericDate(now)
	}
	if c.IssuedAt == 0 {
		c.IssuedAt = jose.NewNumericDate(now)
	}
	if c.ID == "" && ctx.IsSet("jti") {
		if c.ID, err = utils.RandHex(40); err != nil {
			return errors.Wrap(err, "error creating random jti")
		}
	}

	// Validate recommended claims
	if !isSubtle {
		switch {
		case len(c.Issuer) == 0:
			return errors.New("flag '--iss' is required unless '--subtle' is used")
		case len(c.Audience) == 0:
			return errors.New("flag '--aud' is required unless '--subtle' is used")
		case len(c.Subject) == 0:
			return errors.New("flag '--sub' is required unless '--subtle' is used")
		case c.Expiry == 0:
			return errors.New("flag '--exp' is required unless '--subtle' is used")
		case c.Expiry.Time().Before(time.Now()):
			return errors.New("flag '--exp' must be in the future unless '--subtle' is used")
		}
	}

	// Sign
	so := new(jose.SignerOptions)
	so.WithType("JWT")
	if !ctx.Bool("no-kid") && jwk.KeyID != "" {
		so.WithHeader("kid", jwk.KeyID)
	}

	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.SignatureAlgorithm(jwk.Algorithm),
		Key:       jwk.Key,
	}, so)
	if err != nil {
		return errors.Wrapf(err, "error creating JWT signer")
	}

	// Some implementations only accept "aud" as a string.
	// Using claim overwriting for this special case.
	aud := make(map[string]interface{})
	if len(c.Audience) == 1 {
		aud["aud"] = c.Audience[0]
	}

	raw, err := jose.Signed(signer).Claims(c).Claims(aud).Claims(payload).CompactSerialize()
	if err != nil {
		return errors.Wrapf(err, "error serializing JWT")
	}

	fmt.Println(raw)
	return nil
}

func readPayload(filename string) (interface{}, error) {
	var r io.Reader
	if filename == "-" {
		r = os.Stdin
	} else {
		b, err := ioutil.ReadFile(filename)
		if err != nil {
			return nil, errs.FileError(err, filename)
		}
		r = bytes.NewReader(b)
	}

	v := make(map[string]interface{})
	if err := json.NewDecoder(r).Decode(&v); err != nil {
		if filename == "-" {
			return nil, errors.Wrap(err, "error decoding JSON from STDIN")
		}
		return nil, errors.Wrapf(err, "error decoding JSON from %s", filename)
	}
	return v, nil
}