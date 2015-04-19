package opts

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"
)

//Opts is the main class, it contains
//all parsing state for a single set of
//arguments
type Opts struct {
	config       reflect.Value
	parent       *Opts
	subs         map[string]*Opts
	opts         []*option
	args         []*argument
	shorts       map[string]bool
	order        []string
	templates    map[string]string
	internalOpts struct {
		//pretend these are in the user struct :)
		Help, Version bool
	}
	cfgPath string
	useEnv  bool
	erred   error
	cmdname *reflect.Value

	name, version string
	repo, author  string
	//public format settings
	LineWidth int  //42
	PadAll    bool //true
	PadWidth  int  //2
}

//option is the structure representing a
//single flag, exposed for templating
type option struct {
	val         reflect.Value
	name        string
	shortName   string
	displayName string //calculated
	typeName    string
	help        string
}

type argument struct {
	val    reflect.Value
	name   string
	help   string
	hasdef bool
}

//AutoParse creates a new Opts instance and then
//attempts to infer the package name and repo
//from the config's import path and then
//parses the default command line options
func AutoParse(config interface{}) *Opts {
	return autoNew(true, config).Parse()
}

//Parse creates a new Opts instance
func Parse(config interface{}) *Opts {
	return autoNew(false, config).Parse()
}

//New creates a new Opts instance
func New(config interface{}) *Opts {
	return autoNew(false, config)
}

//AutoNew creates a new Opts instance and then
//attempts to infer the package name and repo
//from the config's import path
func AutoNew(config interface{}) *Opts {
	return autoNew(true, config)
}

func autoNew(auto bool, config interface{}) *Opts {
	v := reflect.ValueOf(config)
	//nil parent -> root command
	o := fork(nil, v)
	if o.erred != nil {
		return o
	}

	if auto {
		//auto infer package name and repo or author
		pkgpath := v.Elem().Type().PkgPath()
		parts := strings.Split(pkgpath, "/")
		if len(parts) < 3 {
			o.errorf("Failed to auto-detect package name."+
				" Try moving your %s struct out of the main package.",
				v.Elem().Type().Name())
			return o
		}
		domain := parts[0]
		author := parts[1]
		o.Name(parts[2])
		switch domain {
		case "github.com":
			o.Repo("https://" + strings.Join(parts[0:3], "/"))
		default:
			o.Author(author)
		}
	}

	return o
}

func fork(parent *Opts, c reflect.Value) *Opts {
	//copy default ordering
	order := make([]string, len(DefaultOrder))
	copy(order, DefaultOrder)

	//instantiate
	o := &Opts{
		config:    c,
		parent:    parent,
		shorts:    map[string]bool{},
		subs:      map[string]*Opts{},
		opts:      []*option{},
		order:     order,
		templates: map[string]string{},
		//public defaults
		LineWidth: 72,
		PadAll:    true,
		PadWidth:  2,
	}

	t := c.Type()
	k := t.Kind()
	//must be pointer (meaningless to modify a copy of the struct)
	if k != reflect.Ptr {
		o.errorf("opts: %s should be a pointer to a struct", t.Name())
		return o
	}

	c = c.Elem()
	t = c.Type()
	k = t.Kind()
	if k != reflect.Struct {
		o.errorf("opts: %s should be a pointer to a struct (got %s)", t.Name(), k)
		return o
	}

	//parse struct fields
	for i := 0; i < c.NumField(); i++ {
		val := c.Field(i)
		sf := t.Field(i)
		k := sf.Type.Kind()
		switch k {
		case reflect.Ptr, reflect.Struct:
			o.addSubcmd(sf, val)
		case reflect.Bool, reflect.String, reflect.Int:
			if sf.Tag.Get("cmd") != "" {
				if k != reflect.String {
					o.errorf("'cmd' field '%s' must be a string", sf.Name)
					return o
				}
				o.cmdname = &val
			} else {
				o.addOptArg(sf, val)
			}
		default:
			o.errorf("Struct field '%s' has unsupported type: %s", sf.Name, k)
			return o
		}
	}

	//add help option
	g := reflect.ValueOf(&o.internalOpts).Elem()
	o.addOptArg(g.Type().Field(0), g.Field(0))

	return o
}

func (o *Opts) errorf(format string, args ...interface{}) {
	o.erred = fmt.Errorf(format, args...)
}

func (o *Opts) addSubcmd(sf reflect.StructField, val reflect.Value) {
	//requires address
	switch sf.Type.Kind() {
	case reflect.Ptr:
		//if nil ptr, auto-create new struct
		if val.IsNil() {
			ptr := reflect.New(val.Type().Elem())
			val.Set(ptr)
		}
	case reflect.Struct:
		val = val.Addr()
	}

	subname := camel2dash(sf.Name)
	// log.Printf("define subcmd: %s =====", subname)
	sub := fork(o, val)
	sub.name = subname
	o.subs[subname] = sub
}

func (o *Opts) addOptArg(sf reflect.StructField, val reflect.Value) {
	//assume opt, unless arg tag is present
	t := "opt"
	if sf.Tag.Get("arg") != "" {
		t = "arg"
	}
	//find name
	name := sf.Tag.Get(t)
	if name == "" || name == "!" {
		name = camel2dash(sf.Name) //default to struct field name
	}

	//get help text
	help := sf.Tag.Get("help")

	//display default, when non-zero-val
	hasdef := false
	def := val.Interface()
	if def != reflect.Zero(sf.Type).Interface() {
		hasdef = true
		if help != "" {
			help += " "
		}
		help += fmt.Sprintf("(default %v)", def)
	}

	switch t {
	case "opt":
		n := name[0:1]
		if _, ok := o.shorts[n]; ok {
			n = ""
		} else {
			o.shorts[n] = true
		}
		// log.Printf("define option: %s %s", name, sf.Type)
		o.opts = append(o.opts, &option{
			val:       val,
			name:      name,
			shortName: n,
			help:      help,
		})
	case "arg":
		o.args = append(o.args, &argument{
			val:    val,
			name:   name,
			help:   help,
			hasdef: hasdef,
		})
	default:
		o.errorf("Invalid optype: %s", t)
	}
}

func (o *Opts) Name(name string) *Opts {
	o.name = name
	return o
}

func (o *Opts) Version(version string) *Opts {
	//add version option
	g := reflect.ValueOf(&o.internalOpts).Elem()
	o.addOptArg(g.Type().Field(1), g.Field(1))
	o.order = append(o.order, "version")
	o.version = version
	return o
}

func (o *Opts) Repo(repo string) *Opts {
	o.repo = repo
	o.order = append(o.order, "repo")
	return o
}

func (o *Opts) Author(author string) *Opts {
	o.author = author
	o.order = append(o.order, "author")
	return o
}

//Doc inserts a text block at the end of the help text
func (o *Opts) Doc(paragraph string) *Opts {
	return o
}

//DocAfter inserts a text block after the specified help entry
func (o *Opts) DocAfter(id, paragraph string) *Opts {
	return o
}

//DecSet replaces the specified
func (o *Opts) DocSet(id, paragraph string) *Opts {
	return o
}

//ConfigPath defines a path to a JSON file which matches
//the structure of the provided config. Environment variables
//override JSON Config variables.
func (o *Opts) ConfigPath(path string) *Opts {
	o.cfgPath = path
	return o
}

//ConfigPath defines a path to a JSON file which matches
//the structure of the provided config. Environment variables
//override JSON Config variables.
func (o *Opts) UseEnv() *Opts {
	o.useEnv = true
	return o
}

//Parse with os.Args
func (o *Opts) Parse() *Opts {
	return o.ParseArgs(os.Args[1:])
}

//ParseArgs with the provided arguments
func (o *Opts) ParseArgs(args []string) *Opts {
	if err := o.Process(args); err != nil {
		fmt.Fprint(os.Stderr, err.Error()+"\n")
		os.Exit(1)
	}
	return o
}

//Process is the same as ParseArgs except
//it returns an error instead of calling log.Fatal
func (o *Opts) Process(args []string) error {

	//already errored
	if o.erred != nil {
		return o.erred
	}

	//1. set config via JSON file
	if o.cfgPath != "" {
		b, err := ioutil.ReadFile(o.cfgPath)
		if err == nil {
			v := o.config.Interface()
			err = json.Unmarshal(b, v) //*struct
			if err != nil {
				return fmt.Errorf("Invalid config file: %s", err)
			}
		}
	}

	flagset := flag.NewFlagSet(o.name, flag.ContinueOnError)
	flagset.Usage = func() {
		fmt.Fprint(os.Stderr, o.Help())
		os.Exit(1)
	}

	for _, opt := range o.opts {
		// log.Printf("parse prepare option: %s", opt.name)

		//2. set config via environment
		envVal := ""
		if o.useEnv {
			envName := camel2const(opt.name)
			envVal = os.Getenv(envName)
		}

		//3. set config via Go's pkg/flags
		addr := opt.val.Addr().Interface()
		switch addr := addr.(type) {
		case *bool:
			str2bool(envVal, addr)
			flagset.BoolVar(addr, opt.name, *addr, "")
			if opt.shortName != "" {
				flagset.BoolVar(addr, opt.shortName, *addr, "")
			}
		case *string:
			str2str(envVal, addr)
			flagset.StringVar(addr, opt.name, *addr, "")
			if opt.shortName != "" {
				flagset.StringVar(addr, opt.shortName, *addr, "")
			}
		case *int:
			str2int(envVal, addr)
			flagset.IntVar(addr, opt.name, *addr, "")
			if opt.shortName != "" {
				flagset.IntVar(addr, opt.shortName, *addr, "")
			}
		default:
			panic("Unexpected logic error")
		}
	}

	// log.Printf("parse %+v", args)
	//set user config
	err := flagset.Parse(args)
	if err != nil {
		return err
	}

	//internal opts
	if o.internalOpts.Help {
		flagset.Usage()
	} else if o.internalOpts.Version {
		fmt.Println(o.version)
		os.Exit(0)
	}

	//fill args
	args = flagset.Args()
	for i, argument := range o.args {
		if len(args) > 0 {
			str := args[0]
			args = args[1:]
			argument.val.SetString(str)
		} else if !argument.hasdef {
			//not-set and no default!
			return fmt.Errorf("Argument #%d '%s' is missing", i+1, argument.name)
		}
	}

	//peek at args, maybe use subcommand
	if len(args) > 0 {
		a := args[0]
		//matching subcommand, use it
		if sub, exists := o.subs[a]; exists {
			//user wants name to be set
			if o.cmdname != nil {
				o.cmdname.SetString(a)
			}
			return sub.Process(args[1:])
		}
	}

	return nil
}
