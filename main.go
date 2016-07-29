package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/pborman/getopt"
	"github.com/starkandwayne/goutils/ansi"

	"github.com/starkandwayne/safe/auth"
	"github.com/starkandwayne/safe/dns"
	"github.com/starkandwayne/safe/rc"
	"github.com/starkandwayne/safe/vault"
)

var Version string

func connect() *vault.Vault {
	addr := os.Getenv("VAULT_ADDR")
	if addr == "" {
		ansi.Fprintf(os.Stderr, "@R{You are not targeting a Vault.}\n")
		ansi.Fprintf(os.Stderr, "Try @C{safe target http://your-vault alias}\n")
		ansi.Fprintf(os.Stderr, " or @C{safe target alias}\n")
		os.Exit(1)
	}

	if os.Getenv("VAULT_TOKEN") == "" {
		ansi.Fprintf(os.Stderr, "@R{You are not authenticated to a Vault.}\n")
		ansi.Fprintf(os.Stderr, "Try @C{safe auth ldap}\n")
		ansi.Fprintf(os.Stderr, " or @C{safe auth github}\n")
		ansi.Fprintf(os.Stderr, " or @C{safe auth token}\n")
		os.Exit(1)
	}

	return vault.NewVault(addr, os.Getenv("VAULT_TOKEN"), os.Getenv("VAULT_SKIP_VERIFY") != "")
}

func connectAll(hosts []string) []*vault.Vault {
	vaults := make([]*vault.Vault, len(hosts))
	for i, host := range hosts {
		vaults[i] = vault.NewVault(host, os.Getenv("VAULT_TOKEN"), os.Getenv("VAULT_SKIP_VERIFY") != "")
	}
	return vaults
}

func main() {
	go Signals()

	r := NewRunner()
	r.Dispatch("version", func(command string, args ...string) error {
		if Version != "" {
			fmt.Fprintf(os.Stderr, "safe v%s\n", Version)
		} else {
			fmt.Fprintf(os.Stderr, "safe (development build)\n")
		}
		os.Exit(0)
		return nil
	})

	r.Dispatch("help", func(command string, args ...string) error {
		fmt.Fprintf(os.Stderr, `Usage: safe <cmd> <args ...>

    Valid subcommands are:

    targets
           List all Vaults that have been targeted.

    target [vault-address] name
           Target a new or existing Vault.

    auth [token|ldap|github]
           Authenticate against the currently targeted Vault.

    get path [path ...]
           Retrieve and print the values of one or more paths.

    set path key[=value] [key ...]
           Update a single path with new keys.  Any existing keys that are
           not specified on the command line are left intact. You will be
           prompted to enter values for any keys that do not have values.
           This can be used for more sensitive credentials like passwords,
           PINs, etc.

    paths path [path ... ]
           Provide a flat listing of all reachable keys for each path.

    tree path [path ...]
           Provide a tree hierarchy listing of all reachable keys for each path.

    delete path [path ...]
           Remove multiple paths from the Vault.

    move oldpath newpath
           Move a secret from oldpath to newpath, a rename of sorts.

    copy oldpath newpath
           Copy a secret from oldpath to newpath.

    fmt format_type path oldkey newkey
           Take the value found at path:oldkey, and reformat it based
           on the provided flags (such as base64 encoding or crypt
           hashing). The resultant value will be stored into path:newkey.

           Valid format_types include the following:
           - crypt-sha512
           - base64

    gen [length] path key
           Generate a new, random password (length defaults to 64 chars).

    ssh [nbits] path [path ...]
           Generate a new SSH RSA keypair, adding the keys "private" and
           "public" to each path. The public key will be encoded as an
           authorized keys. The private key is a PEM-encoded DER private
           key. (nbits defaults to 2048 bits)

    rsa [nbits] path [path ...]
           Generate a new RSA keypair, adding the keys "private" and "public"
           to each path. Both keys will be PEM-encoded DER. (nbits defaults
           to 2048 bits)

    prompt ...
           Echo the arguments, space-separated, as a single line to the terminal.

    import <export.file
           Read from STDIN an export file and write all of the secrets contained
           therein to the same paths inside the Vault

    export path [path ...]
           Export the given subtree(s) in a format suitable for migration (via a
           future import call), or long-term storage offline.

    vault  ...
           Runs arbitrary commands through the vault cli.
`)
		os.Exit(0)
		return nil
	})

	r.Dispatch("targets", func(command string, args ...string) error {
		if len(args) != 0 {
			return fmt.Errorf("USAGE: targets")
		}

		cfg := rc.Apply(false)
		wide := 0
		var keys []string
		for name, _ := range cfg.Targets {
			keys = append(keys, name)
			if len(name) > wide {
				wide = len(name)
			}
		}

		fmt.Fprintf(os.Stderr, "\n")
		current := fmt.Sprintf("(*) @G{%%-%ds}\t@Y{%%s}\n", wide)
		other := fmt.Sprintf("    %%-%ds\t%%s\n", wide)
		sort.Strings(keys)
		for _, name := range keys {
			if name == cfg.Target {
				ansi.Fprintf(os.Stderr, current, name, cfg.Targets[name].URL)
			} else {
				ansi.Fprintf(os.Stderr, other, name, cfg.Targets[name].URL)
			}
		}
		fmt.Fprintf(os.Stderr, "\n")
		return nil
	})

	r.Dispatch("target", func(command string, args ...string) error {
		cfg := rc.Apply(false)
		if len(args) == 0 {
			if cfg.Target == "" {
				ansi.Fprintf(os.Stderr, "@R{No Vault currently targeted}\n")
			} else {
				ansi.Fprintf(os.Stderr, "Currently targeting @C{%s} at @C{%s}\n", cfg.Target, cfg.URL())
			}
			return nil
		}
		if len(args) == 1 {
			err := cfg.SetCurrent(args[0])
			if err != nil {
				return err
			}
			ansi.Fprintf(os.Stderr, "Now targeting @C{%s} at @C{%s}\n", cfg.Target, cfg.URL())
			return cfg.Write()
		}

		if len(args) == 2 {
			var err error
			if strings.HasPrefix(args[1], "http://") || strings.HasPrefix(args[1], "https://") {
				err = cfg.SetTarget(args[0], args[1])
			} else {
				err = cfg.SetTarget(args[1], args[0])
			}
			if err != nil {
				return err
			}
			ansi.Fprintf(os.Stderr, "Now targeting @C{%s} at @C{%s}\n", cfg.Target, cfg.URL())
			return cfg.Write()
		}

		return fmt.Errorf("USAGE: target [vault-address] name")
	})

	r.Dispatch("env", func(command string, args ...string) error {
		rc.Apply(true)
		ansi.Fprintf(os.Stderr, "  @B{VAULT_ADDR}  @G{%s}\n", os.Getenv("VAULT_ADDR"))
		ansi.Fprintf(os.Stderr, "  @B{VAULT_TOKEN} @G{%s}\n", os.Getenv("VAULT_TOKEN"))
		return nil
	})

	r.Dispatch("auth", func(command string, args ...string) error {
		cfg := rc.Apply(true)

		method := "token"
		if len(args) > 0 {
			method = args[0]
			args = args[1:]
		}

		var token string
		var err error

		ansi.Fprintf(os.Stderr, "Authenticating against @C{%s} at @C{%s}\n", cfg.Target, cfg.URL())
		switch method {
		case "token":
			token, err = auth.Token(os.Getenv("VAULT_ADDR"))
			if err != nil {
				return err
			}
			break

		case "ldap":
			token, err = auth.LDAP(os.Getenv("VAULT_ADDR"))
			if err != nil {
				return err
			}
			break

		case "github":
			token, err = auth.Github(os.Getenv("VAULT_ADDR"))
			if err != nil {
				return err
			}
			break

		default:
			return fmt.Errorf("Unrecognized authentication method '%s'", method)
		}

		cfg.SetToken(token)
		return cfg.Write()

	}, "login")

	r.Dispatch("sync", func(command string, args ...string) error {
		rc.Apply(true)
		return nil
	})

	r.Dispatch("status", func(command string, args ...string) error {
		cfg := rc.Apply(true)

		if len(args) != 0 {
			return fmt.Errorf("USAGE: status")
		}

		backends := cfg.VaultEndpoints()
		if len(backends) == 0 {
			return fmt.Errorf("No backends detected")
		}

		for _, v := range connectAll(backends) {
			sealed, _, err := v.CheckSeal()
			if err != nil {
				ansi.Fprintf(os.Stderr, "%s: @R{%s}\n", v.URL, err)
			} else if sealed {
				ansi.Fprintf(os.Stderr, "%s: @C{SEALED}\n", v.URL)
			} else {
				ansi.Fprintf(os.Stderr, "%s: @G{unsealed}\n", v.URL)
			}
		}
		return nil
	})

	r.Dispatch("seal", func(command string, args ...string) error {
		cfg := rc.Apply(true)

		if len(args) != 0 {
			return fmt.Errorf("USAGE: seal")
		}

		servers := cfg.DNS()
		if len(servers) == 0 {
			return fmt.Errorf("No backends detected")
		}

		/* while there are vault.service.consul */
		fmt.Fprintf(os.Stderr, "looking up unsealed vaults to seal...\n")
		for dns.HasRecordsFor("vault.service.consul", servers) {
			/* wait until there is a active.vault.service.consul entry */
			active, ok := dns.WaitForChange("active.vault.service.consul", "", 300, servers)
			if !ok {
				return fmt.Errorf("Timed out determining the active vault node")
			}
			ansi.Printf("@Y{active node is now %s}\n", active)

			/* seal */

			/* FIXME: this is a terrible way of doing this */
			u, err := url.Parse(cfg.Targets[cfg.Target].URL)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "sealing host %s\n", rc.SwapHost(u, active))
			v := vault.NewVault(rc.SwapHost(u, active), os.Getenv("VAULT_TOKEN"), os.Getenv("VAULT_SKIP_VERIFY") != "")
			if err := v.Seal(); err != nil {
				return fmt.Errorf("%s failed: %s\n", v.URL, err)
			}

			/* wait until the active.vault.service.consul entry changes */
			_, ok = dns.WaitForChange("active.vault.service.consul", active, 30, servers)
			if !ok {
				return fmt.Errorf("Timed out waiting for a new active vault node")
			}
		}

		ansi.Fprintf(os.Stderr, "The Vaults are sealed!\n")
		return nil
	})

	r.Dispatch("unseal", func(command string, args ...string) error {
		cfg := rc.Apply(true)
		if len(args) != 0 {
			return fmt.Errorf("USAGE: unseal")
		}

		backends := cfg.VaultEndpoints()
		if len(backends) == 0 {
			return fmt.Errorf("No backends detected")
		}

		keys := []string{} // seal keys, to be provided by user
		for _, v := range connectAll(backends) {
			sealed, threshold, err := v.CheckSeal()
			if err != nil {
				return err
			}
			if !sealed {
				continue
			}

			if len(keys) == 0 {
				for i := 0; i < threshold; i++ {
					keys = append(keys, pr(ansi.Sprintf("Seal Key @M{#%d}", i+1), false))
				}
			}

			v.Unseal(keys)
		}

		if len(keys) != 0 {
			ansi.Fprintf(os.Stderr, "Unsealed the Vault(s)\n")
		} else {
			ansi.Fprintf(os.Stderr, "Vaults are already unsealed; taking no action.\n")
		}
		return nil
	})

	r.Dispatch("set", func(command string, args ...string) error {
		rc.Apply(true)
		if len(args) < 2 {
			return fmt.Errorf("USAGE: set path key[=value] [key ...]")
		}
		v := connect()
		path, args := args[0], args[1:]
		s, err := v.Read(path)
		if err != nil && err != vault.NotFound {
			return err
		}
		for _, set := range args {
			k, v, err := keyPrompt(set, true)
			if err != nil {
				return err
			}
			s.Set(k, v)
		}
		return v.Write(path, s)
	}, "write")

	r.Dispatch("paste", func(command string, args ...string) error {
		rc.Apply(true)
		if len(args) < 2 {
			return fmt.Errorf("USAGE: set path key[=value] [key ...]")
		}
		v := connect()
		path, args := args[0], args[1:]
		s, err := v.Read(path)
		if err != nil && err != vault.NotFound {
			return err
		}
		for _, set := range args {
			k, v, err := keyPrompt(set, false)
			if err != nil {
				return err
			}
			s.Set(k, v)
		}
		return v.Write(path, s)
	})

	r.Dispatch("get", func(command string, args ...string) error {
		rc.Apply(true)
		if len(args) < 1 {
			return fmt.Errorf("USAGE: get path [path ...]")
		}
		v := connect()
		for _, path := range args {
			s, err := v.Read(path)
			if err != nil {
				return err
			}
			fmt.Printf("--- # %s\n", path)
			fmt.Printf("%s\n\n", s.YAML())
		}
		return nil
	}, "read", "cat")

	r.Dispatch("tree", func(command string, args ...string) error {
		rc.Apply(true)
		if len(args) == 0 {
			args = append(args, "secret")
		}
		v := connect()
		for _, path := range args {
			tree, err := v.Tree(path, true)
			if err != nil {
				return err
			}
			fmt.Printf("%s\n", tree.Draw())
		}
		return nil
	})

	r.Dispatch("paths", func(command string, args ...string) error {
		rc.Apply(true)
		if len(args) < 1 {
			return fmt.Errorf("USAGE: paths path [path ...]")
		}
		v := connect()
		for _, path := range args {
			tree, err := v.Tree(path, false)
			if err != nil {
				return err
			}
			for _, s := range tree.Paths("/") {
				fmt.Printf("%s\n", s)
			}
		}
		return nil
	})

	r.Dispatch("delete", func(command string, args ...string) error {
		rc.Apply(true)

		recurse, args := shouldRecurse(command, args...)

		if len(args) < 1 {
			return fmt.Errorf("USAGE: delete path [path ...]")
		}
		v := connect()
		for _, path := range args {
			if recurse {
				if err := v.DeleteTree(path); err != nil {
					return err
				}
			} else {
				if err := v.Delete(path); err != nil {
					return err
				}
			}
		}
		return nil
	}, "rm")

	r.Dispatch("export", func(command string, args ...string) error {
		rc.Apply(true)
		if len(args) < 1 {
			return fmt.Errorf("USAGE: export path [path ...]")
		}
		v := connect()
		data := make(map[string]*vault.Secret)
		for _, path := range args {
			tree, err := v.Tree(path, false)
			if err != nil {
				return err
			}
			for _, sub := range tree.Paths("/") {
				s, err := v.Read(sub)
				if err != nil {
					return err
				}
				data[sub] = s
			}
		}

		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", string(b))

		return nil
	})

	r.Dispatch("import", func(command string, args ...string) error {
		rc.Apply(true)
		b, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		var data map[string]*vault.Secret
		err = json.Unmarshal(b, &data)
		if err != nil {
			return err
		}

		v := connect()
		for path, s := range data {
			err = v.Write(path, s)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s\n", path)
		}
		return nil
	})

	r.Dispatch("move", func(command string, args ...string) error {
		rc.Apply(true)

		recurse, args := shouldRecurse(command, args...)

		if len(args) != 2 {
			return fmt.Errorf("USAGE: move oldpath newpath", args)
		}
		v := connect()

		if recurse {
			if err := v.MoveCopyTree(args[0], args[1], v.Move); err != nil {
				return err
			}
		} else {
			if err := v.Move(args[0], args[1]); err != nil {
				return err
			}
		}
		return nil
	}, "mv", "rename")

	r.Dispatch("copy", func(command string, args ...string) error {
		rc.Apply(true)

		recurse, args := shouldRecurse(command, args...)

		if len(args) != 2 {
			return fmt.Errorf("USAGE: copy oldpath newpath", args)
		}
		v := connect()

		if recurse {
			if err := v.MoveCopyTree(args[0], args[1], v.Copy); err != nil {
				return err
			}
		} else {
			if err := v.Copy(args[0], args[1]); err != nil {
				return err
			}
		}
		return nil
	}, "cp")

	r.Dispatch("gen", func(command string, args ...string) error {
		rc.Apply(true)
		length := 64
		if len(args) > 0 {
			if u, err := strconv.ParseUint(args[0], 10, 16); err == nil {
				length = int(u)
				args = args[1:]
			}
		}

		if len(args) != 2 {
			return fmt.Errorf("USAGE: gen [length] path key")
		}

		v := connect()
		path, key := args[0], args[1]
		s, err := v.Read(path)
		if err != nil && err != vault.NotFound {
			return err
		}
		s.Password(key, length)

		if err = v.Write(path, s); err != nil {
			return err
		}
		return nil
	}, "auto")

	r.Dispatch("ssh", func(command string, args ...string) error {
		rc.Apply(true)
		bits := 2048
		if len(args) > 0 {
			if u, err := strconv.ParseUint(args[0], 10, 16); err == nil {
				bits = int(u)
				args = args[1:]
			}
		}

		if len(args) < 1 {
			return fmt.Errorf("USAGE: ssh [bits] path [path ...]")
		}

		v := connect()
		for _, path := range args {
			s, err := v.Read(path)
			if err != nil && err != vault.NotFound {
				return err
			}
			if err = s.SSHKey(bits); err != nil {
				return err
			}
			if err = v.Write(path, s); err != nil {
				return err
			}
		}
		return nil
	})

	r.Dispatch("rsa", func(command string, args ...string) error {
		rc.Apply(true)
		bits := 2048
		if len(args) > 0 {
			if u, err := strconv.ParseUint(args[0], 10, 16); err == nil {
				bits = int(u)
				args = args[1:]
			}
		}

		if len(args) < 1 {
			return fmt.Errorf("USAGE: rsa [bits] path [path ...]")
		}

		v := connect()
		for _, path := range args {
			s, err := v.Read(path)
			if err != nil && err != vault.NotFound {
				return err
			}
			if err = s.RSAKey(bits); err != nil {
				return err
			}
			if err = v.Write(path, s); err != nil {
				return err
			}
		}
		return nil
	})

	r.Dispatch("prompt", func(command string, args ...string) error {
		fmt.Fprintf(os.Stderr, "%s\n", strings.Join(args, " "))
		return nil
	})

	r.Dispatch("vault", func(command string, args ...string) error {
		rc.Apply(true)

		cmd := exec.Command("vault", args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Run()
		if err != nil {
			return err
		}
		return nil
	})

	r.Dispatch("fmt", func(command string, args ...string) error {
		rc.Apply(true)

		if len(args) != 4 {
			return fmt.Errorf("USAGE: fmt format_type path oldkey newkey")
		}

		fmtType := args[0]
		path := args[1]
		oldKey := args[2]
		newKey := args[3]

		v := connect()
		s, err := v.Read(path)
		if err != nil {
			return err
		}
		if err = s.Format(oldKey, newKey, fmtType); err != nil {
			if err == vault.NotFound {
				return fmt.Errorf("%s:%s does not exist, cannot create %s encoded copy at %s:%s", path, oldKey, fmtType, path, newKey)
			}
			return fmt.Errorf("Error encoding %s:%s as %s: %s", path, oldKey, fmtType, err)
		}

		return v.Write(path, s)
	})

	insecure := getopt.BoolLong("insecure", 'k', "Disable SSL/TLS certificate validation")
	showVersion := getopt.BoolLong("version", 'v', "Print version information and exit")
	showHelp := getopt.BoolLong("help", 'h', "Get some help")
	opts := getopt.CommandLine
	opts.Parse(os.Args)

	var args []string
	if *showHelp {
		args = []string{"help"}

	} else if *showVersion {
		args = []string{"version"}

	} else if opts.NArgs() == 0 {
		args = []string{"help"}

	} else {
		args = opts.Args()
	}

	if *insecure {
		os.Setenv("VAULT_SKIP_VERIFY", "1")
	}

	if err := r.Run(args...); err != nil {
		if strings.HasPrefix(err.Error(), "USAGE") {
			ansi.Fprintf(os.Stderr, "@Y{%s}\n", err)
		} else {
			ansi.Fprintf(os.Stderr, "@R{!! %s}\n", err)
		}
		os.Exit(1)
	}
}

func shouldRecurse(cmd string, args ...string) (bool, []string) {
	var recursiveMode, forceMode *bool

	forceMode = getopt.BoolLong("force", 'f', "Disable confirmation prompting")
	recursiveMode = getopt.BoolLong("recursive", 'R', "Enable recursion")

	args = append([]string{"safe " + cmd}, args...)

	var opts = getopt.CommandLine
	var parsed []string
	for {
		opts.Parse(args)
		if opts.NArgs() == 0 {
			break
		}
		parsed = append(parsed, opts.Arg(0))
		args = opts.Args()
	}

	args = parsed

	if *recursiveMode && !*forceMode {
		fmt.Printf("Are you sure you wish to recursively %s %s? (y/n) ", cmd, strings.Join(args, " "))
		reader := bufio.NewReader(os.Stdin)
		y, _ := reader.ReadString('\n')
		y = strings.TrimSpace(y)
		if y != "y" && y != "yes" {
			fmt.Printf("Aborting...\n")
			os.Exit(0)
		}
	}

	return *recursiveMode, args
}
