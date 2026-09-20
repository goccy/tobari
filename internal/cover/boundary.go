package cover

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/types"
	"sort"

	"golang.org/x/tools/go/ssa"
)

// Boundaries: how the code under measurement uses each package outside it.
//
// The code under development — coverage targets, the main/test packages, and
// every other package that no module version pins, "inside" below; see
// isInsidePkg — is the only code that changes from one build of a target to
// the next while its dependencies stay the same. Whatever a
// dependency does on behalf of the program, it does because inside code used
// it: called one of its functions, handed it a value, stored into one of its
// variables or fields. The boundary of an outside package is the set of those
// uses. While the boundary of a package is unchanged, so is everything the
// inside code can make that package, and the packages it uses in turn, do;
// what was learned about them in an earlier build still holds.
//
// A use is recorded with the inside function it occurs in, what it refers to,
// and the values handed over: their types — the concrete type when a value is
// converted to an interface on the spot, since that is what decides which of
// the program's methods the dependency can end up calling — or, for a
// function, its name. Source positions are left out, so editing a function
// body around its uses, or moving code within a file, changes nothing.

// boundaryUse is one use of an outside package by an inside function.
type boundaryUse struct {
	// Kind is one of the boundaryUse* constants.
	Kind string `json:"kind"`
	// In is the inside function containing the use.
	In string `json:"in"`
	// Of is what is used: a function, a method, a variable, a struct field or
	// an interface type of the outside package.
	Of string `json:"of"`
	// With describes the values handed over, if any; see handedType.
	With []string `json:"with,omitempty"`
}

const (
	// boundaryUseCall is a static call of an outside function or method.
	boundaryUseCall = "call"
	// boundaryUseInvoke is a call through a method of an outside interface.
	boundaryUseInvoke = "invoke"
	// boundaryUseRef is a reference to an outside function or variable other
	// than as the callee of a static call.
	boundaryUseRef = "ref"
	// boundaryUseStore is a store into a field of an outside struct type.
	boundaryUseStore = "store"
	// boundaryUseConvert is the conversion of a value to an outside interface.
	boundaryUseConvert = "convert"
)

// boundaries maps the import path of an outside package to the fingerprint of
// its boundary. A package the inside code does not use has no entry.
type boundaries map[string]string

// changedFrom returns the packages whose boundary differs between b and prev,
// including the packages only one of them has an entry for.
func (b boundaries) changedFrom(prev boundaries) []string {
	var changed []string
	for pkgPath, fingerprint := range b {
		if prev[pkgPath] != fingerprint {
			changed = append(changed, pkgPath)
		}
	}
	for pkgPath := range prev {
		if _, ok := b[pkgPath]; !ok {
			changed = append(changed, pkgPath)
		}
	}
	sort.Strings(changed)
	return changed
}

// computeBoundaries fingerprints the uses the inside packages make of every
// other package. The inside packages must already be built.
func computeBoundaries(prog *ssa.Program, inside []*ssa.Package) boundaries {
	isInside := make(map[*types.Package]struct{}, len(inside))
	for _, ssaPkg := range inside {
		isInside[ssaPkg.Pkg] = struct{}{}
	}
	c := &boundaryCollector{isInside: isInside, uses: make(map[string]map[string]struct{})}
	for _, ssaPkg := range inside {
		for _, fn := range packageFunctions(prog, ssaPkg) {
			c.collect(fn)
		}
	}

	result := make(boundaries, len(c.uses))
	for pkgPath, uses := range c.uses {
		h := sha256.New()
		for _, use := range sortedKeys(uses) {
			h.Write([]byte(use))
			h.Write([]byte{'\n'})
		}
		result[pkgPath] = hex.EncodeToString(h.Sum(nil))[:32]
	}
	return result
}

// packageFunctions returns every function with a body that belongs to ssaPkg:
// its package-level functions (including init), the methods of its named
// types, and the function literals nested in any of them.
func packageFunctions(prog *ssa.Program, ssaPkg *ssa.Package) []*ssa.Function {
	var (
		fns  []*ssa.Function
		seen = make(map[*ssa.Function]struct{})
		add  func(fn *ssa.Function)
	)
	add = func(fn *ssa.Function) {
		if fn == nil || fn.Blocks == nil {
			return
		}
		if _, ok := seen[fn]; ok {
			return
		}
		seen[fn] = struct{}{}
		fns = append(fns, fn)
		for _, anon := range fn.AnonFuncs {
			add(anon)
		}
	}
	for _, name := range sortedKeys(ssaPkg.Members) {
		switch member := ssaPkg.Members[name].(type) {
		case *ssa.Function:
			add(member)
		case *ssa.Type:
			// A generic type has no methods of its own to build; the methods
			// of its instantiations are reached through the calls to them.
			named, ok := member.Type().(*types.Named)
			if !ok || named.TypeParams().Len() != 0 {
				continue
			}
			for _, t := range []types.Type{named, types.NewPointer(named)} {
				methods := prog.MethodSets.MethodSet(t)
				for i := range methods.Len() {
					// Promoted methods of an embedded outside type get a
					// wrapper that belongs to no package; only the methods
					// declared here are inside code.
					if method := prog.MethodValue(methods.At(i)); method != nil && method.Pkg == ssaPkg {
						add(method)
					}
				}
			}
		}
	}
	return fns
}

type boundaryCollector struct {
	isInside map[*types.Package]struct{}
	// uses holds, per outside package, the encoded uses of it.
	uses map[string]map[string]struct{}
}

// outside returns the import path of pkg if it is an outside package. The
// universe (error, any) and unnamed types belong to no package and therefore
// to no boundary.
func (c *boundaryCollector) outside(pkg *types.Package) (string, bool) {
	if pkg == nil {
		return "", false
	}
	if _, ok := c.isInside[pkg]; ok {
		return "", false
	}
	return pkg.Path(), true
}

func (c *boundaryCollector) add(pkgPath string, use boundaryUse) {
	// Encoding the use as JSON keeps its fields apart whatever characters
	// the names contain. A struct of strings cannot fail to encode.
	encoded, err := json.Marshal(use)
	if err != nil {
		panic("tobari: failed to encode a boundary use: " + err.Error())
	}
	uses := c.uses[pkgPath]
	if uses == nil {
		uses = make(map[string]struct{})
		c.uses[pkgPath] = uses
	}
	uses[string(encoded)] = struct{}{}
}

// functionPackage returns the package declaring fn. An instantiation of a
// generic function belongs to no package itself, so its origin decides.
func functionPackage(fn *ssa.Function) *types.Package {
	if fn.Pkg == nil && fn.Origin() != nil {
		fn = fn.Origin()
	}
	if fn.Pkg == nil {
		return nil
	}
	return fn.Pkg.Pkg
}

// handedType describes a value where it is handed over: by its concrete type
// if it is converted to an interface right there, and by name if it is a
// function or a function literal, which says more than its signature about
// what the receiver can end up calling.
func handedType(v ssa.Value) string {
	if mi, ok := v.(*ssa.MakeInterface); ok {
		v = mi.X
	}
	switch v := v.(type) {
	case *ssa.Function:
		return "func " + normalizeFuncName(v)
	case *ssa.MakeClosure:
		if fn, ok := v.Fn.(*ssa.Function); ok {
			return "func " + normalizeFuncName(fn)
		}
	}
	return types.TypeString(v.Type(), nil)
}

func handedTypes(values []ssa.Value) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = handedType(v)
	}
	return out
}

func (c *boundaryCollector) collect(fn *ssa.Function) {
	in := normalizeFuncName(fn)
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			var staticCallee *ssa.Function
			if call, ok := instr.(ssa.CallInstruction); ok {
				common := call.Common()
				staticCallee = common.StaticCallee()
				c.collectCall(in, common, staticCallee)
			}
			switch instr := instr.(type) {
			case *ssa.Store:
				c.collectStore(in, instr)
			case *ssa.MakeInterface:
				c.collectConvert(in, instr)
			}
			c.collectRefs(in, instr, staticCallee)
		}
	}
}

func (c *boundaryCollector) collectCall(in string, common *ssa.CallCommon, staticCallee *ssa.Function) {
	if common.IsInvoke() {
		if pkgPath, ok := c.outside(common.Method.Pkg()); ok {
			c.add(pkgPath, boundaryUse{Kind: boundaryUseInvoke, In: in, Of: common.Method.FullName(), With: handedTypes(common.Args)})
		}
		return
	}
	if staticCallee == nil {
		return
	}
	if pkgPath, ok := c.outside(functionPackage(staticCallee)); ok {
		// The name of an instantiation includes its type arguments.
		c.add(pkgPath, boundaryUse{Kind: boundaryUseCall, In: in, Of: staticCallee.String(), With: handedTypes(common.Args)})
	}
}

// collectRefs records the outside functions and variables an instruction
// refers to, except the callee of a static call, which collectCall covers.
func (c *boundaryCollector) collectRefs(in string, instr ssa.Instruction, staticCallee *ssa.Function) {
	var operands [8]*ssa.Value
	for _, operand := range instr.Operands(operands[:0]) {
		if operand == nil || *operand == nil {
			continue
		}
		switch v := (*operand).(type) {
		case *ssa.Function:
			if v == staticCallee {
				continue
			}
			if pkgPath, ok := c.outside(functionPackage(v)); ok {
				c.add(pkgPath, boundaryUse{Kind: boundaryUseRef, In: in, Of: v.String()})
			}
		case *ssa.Global:
			if v.Pkg == nil {
				continue
			}
			pkgPath, ok := c.outside(v.Pkg.Pkg)
			if !ok {
				continue
			}
			use := boundaryUse{Kind: boundaryUseRef, In: in, Of: v.String()}
			if store, isStore := instr.(*ssa.Store); isStore && store.Addr == v {
				use.With = []string{handedType(store.Val)}
			}
			c.add(pkgPath, use)
		}
	}
}

// collectStore records a store into a field of an outside struct type, which
// is how a callback or an implementation is handed over through a struct
// (&http.Server{Handler: h}).
func (c *boundaryCollector) collectStore(in string, store *ssa.Store) {
	fieldAddr, ok := store.Addr.(*ssa.FieldAddr)
	if !ok {
		return
	}
	ptr, ok := fieldAddr.X.Type().Underlying().(*types.Pointer)
	if !ok {
		return
	}
	named, ok := types.Unalias(ptr.Elem()).(*types.Named)
	if !ok {
		return
	}
	pkgPath, ok := c.outside(named.Obj().Pkg())
	if !ok {
		return
	}
	structType, ok := named.Underlying().(*types.Struct)
	if !ok {
		return
	}
	c.add(pkgPath, boundaryUse{
		Kind: boundaryUseStore,
		In:   in,
		Of:   types.TypeString(named, nil) + "." + structType.Field(fieldAddr.Field).Name(),
		With: []string{handedType(store.Val)},
	})
}

// collectConvert records the conversion of a value to an outside interface
// type, wherever the resulting value goes afterwards.
func (c *boundaryCollector) collectConvert(in string, mi *ssa.MakeInterface) {
	named, ok := types.Unalias(mi.Type()).(*types.Named)
	if !ok {
		return
	}
	if pkgPath, ok := c.outside(named.Obj().Pkg()); ok {
		c.add(pkgPath, boundaryUse{
			Kind: boundaryUseConvert,
			In:   in,
			Of:   types.TypeString(named, nil),
			With: []string{types.TypeString(mi.X.Type(), nil)},
		})
	}
}
