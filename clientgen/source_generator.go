package clientgen

import (
	"fmt"
	"github.com/99designs/gqlgen/codegen/config"
	"github.com/99designs/gqlgen/codegen/templates"
	"github.com/vektah/gqlparser/v2/ast"
	"go/types"
	"math"
	"sort"
	"strings"
)

type FieldPath struct {
	Kind ast.DefinitionKind
	path []string
}

func NewFieldPath(kind ast.DefinitionKind, name string) FieldPath {
	return FieldPath{
		Kind: kind,
		path: []string{name},
	}
}

func (p FieldPath) With(n string) FieldPath {
	p.path = append(p.path, n)
	return p
}

func (p FieldPath) Name() string {
	pn := make([]string, 0, len(p.path))
	for _, n := range p.path {
		pn = append(pn, templates.ToGo(n))
	}

	return strings.Join(pn, "_")
}

func (p FieldPath) String() string {
	return strings.Join(p.path, ".")
}

type Argument struct {
	Variable string
	Type     types.Type
}

type ResponseField struct {
	Name             string
	IsFragmentSpread bool
	IsInlineFragment bool
	Type             types.Type
	Tags             []string
	ResponseFields   ResponseFieldList
}

type ResponseFieldList []*ResponseField

func (rs ResponseFieldList) IsFragment() bool {
	if len(rs) != 1 {
		return false
	}

	return rs[0].IsInlineFragment || rs[0].IsFragmentSpread
}

func (rs ResponseFieldList) IsBasicType() bool {
	return len(rs) == 0
}

func (rs ResponseFieldList) IsStructType() bool {
	return len(rs) > 0 && !rs.IsFragment()
}

type genType struct {
	name string
	typ  *Type
}

type SourceGenerator struct {
	cfg    *config.Config
	binder *config.Binder
	client config.PackageConfig

	genTypes []genType
}

func (r *SourceGenerator) RegisterGenType(name string, typ *Type) {
	if gt := r.GetGenType(name); gt != nil {
		panic(name + ": gen type already defined")
	}

	if typ.RefType == nil {
		typ.RefType = types.NewNamed(
			types.NewTypeName(0, r.client.Pkg(), name, nil),
			nil,
			nil,
		)
	}

	r.genTypes = append(r.genTypes, genType{
		name: name,
		typ:  typ,
	})
}

func (r *SourceGenerator) GetGenType(name string) *Type {
	for _, gt := range r.genTypes {
		if gt.name == name {
			return gt.typ
		}
	}

	return nil
}

func (r *SourceGenerator) GenTypes() []*Type {
	typs := make([]*Type, 0)
	for _, gt := range r.genTypes {
		typs = append(typs, gt.typ)
	}

	sort.SliceStable(typs, func(i, j int) bool {
		pi := typs[i].Path.path
		pj := typs[j].Path.path

		for n := 0; n < int(math.Min(float64(len(pi)), float64(len(pj)))); n++ {
			if pi[n] != pj[n] {
				return pi[n] < pj[n]
			}
		}

		return len(pi) < len(pj)
	})

	return typs
}

func NewSourceGenerator(cfg *config.Config, client config.PackageConfig) *SourceGenerator {
	return &SourceGenerator{
		cfg:    cfg,
		binder: cfg.NewBinder(),
		client: client,
	}
}

func (r *SourceGenerator) NewResponseFields(path FieldPath, selectionSet *ast.SelectionSet) ResponseFieldList {
L:
	for _, field := range *selectionSet {
		switch field.(type) {
		case *ast.InlineFragment:
			*selectionSet = append(ast.SelectionSet{&ast.Field{
				Name:  "__typename",
				Alias: "__typename",
				Definition: &ast.FieldDefinition{
					Name: "Typename",
					Type: ast.NonNullNamedType("String", nil),
					Arguments: ast.ArgumentDefinitionList{
						{Name: "name", Type: ast.NonNullNamedType("String", nil)},
					},
				},
			}}, *selectionSet...)
			break L
		}
	}

	responseFields := make(ResponseFieldList, 0, len(*selectionSet))
	for _, selection := range *selectionSet {
		rf := r.NewResponseField(path, selection)
		responseFields = append(responseFields, rf)
	}

	return responseFields
}

func (r *SourceGenerator) namedType(path FieldPath, gen func() types.Type) types.Type {
	fullname := path.Name()

	if gt := r.GetGenType(fullname); gt != nil {
		return gt.RefType
	}

	if r.cfg.Models.Exists(fullname) {
		model := r.cfg.Models[fullname].Model[0]
		fmt.Printf("%s is already declared: %v\n", fullname, model)

		typ, err := r.binder.FindTypeFromName(model)
		if err != nil {
			panic(fmt.Errorf("cannot get type for %v (%v): %w", fullname, model, err))
		}

		return typ
	} else {
		name := fullname

		genTyp := &Type{
			Name: name,
			Path: path,
		}

		r.RegisterGenType(name, genTyp)

		genTyp.Type = gen()

		return genTyp.RefType
	}
}

func (r *SourceGenerator) genFromResponseFields(path FieldPath, fieldsResponseFields ResponseFieldList) types.Type {
	fullname := path.Name()

	vars := make([]*types.Var, 0, len(fieldsResponseFields))
	tags := make([]string, 0, len(fieldsResponseFields))
	unmarshalTypes := map[string]TypeTarget{}
	for _, field := range fieldsResponseFields {
		typ := field.Type
		fieldName := templates.ToGo(field.Name)
		if field.IsInlineFragment {
			unmarshalTypes[field.Name] = TypeTarget{
				Type: typ,
				Name: fieldName,
			}
			typ = types.NewPointer(typ)
		}

		vars = append(vars, types.NewVar(0, nil, fieldName, typ))
		tags = append(tags, strings.Join(field.Tags, " "))
	}

	genType := r.GetGenType(fullname)
	genType.UnmarshalTypes = unmarshalTypes

	return types.NewStruct(vars, tags)
}

func (r *SourceGenerator) AstTypeToType(path FieldPath, fields ResponseFieldList, typ *ast.Type) types.Type {
	switch {
	case fields.IsBasicType():
		def := r.cfg.Schema.Types[typ.Name()]

		return r.namedType(NewFieldPath(def.Kind, def.Name), func() types.Type {
			return r.genFromDefinition(def)
		})
	case fields.IsFragment():
		// if a child field is fragment, this field type became fragment.
		return fields[0].Type
	case fields.IsStructType():
		return r.namedType(path, func() types.Type {
			return r.genFromResponseFields(path, fields)
		})
	default:
		// ここにきたらバグ
		// here is bug
		panic("not match type")
	}
}

func (r *SourceGenerator) NewResponseField(path FieldPath, selection ast.Selection) *ResponseField {
	switch selection := selection.(type) {
	case *ast.Field:
		fieldPath := path.With(selection.Name)
		fieldsResponseFields := r.NewResponseFields(fieldPath, &selection.SelectionSet)
		baseType := r.AstTypeToType(fieldPath, fieldsResponseFields, selection.Definition.Type)

		// GraphQLの定義がオプショナルのはtypeのポインタ型が返り、配列の定義場合はポインタのスライスの型になって返ってきます
		// return pointer type then optional type or slice pointer then slice type of definition in GraphQL.
		typ := r.binder.CopyModifiersFromAst(selection.Definition.Type, baseType)

		return &ResponseField{
			Name: selection.Alias,
			Type: typ,
			Tags: []string{
				fmt.Sprintf(`json:"%s"`, selection.Alias),
			},
			ResponseFields: fieldsResponseFields,
		}

	case *ast.FragmentSpread:
		fieldsResponseFields := r.NewResponseFields(path, &selection.Definition.SelectionSet)

		name := selection.Definition.Name
		typ := r.namedType(NewFieldPath(selection.ObjectDefinition.Kind, name), func() types.Type {
			panic(fmt.Sprintf("fragment %v must already be generated", name))
		})

		return &ResponseField{
			Name:             selection.Name,
			Type:             typ,
			IsFragmentSpread: true,
			ResponseFields:   fieldsResponseFields,
		}

	case *ast.InlineFragment:
		// InlineFragmentは子要素をそのままstructとしてもつので、ここで、構造体の型を作成します
		path := path.With(selection.TypeCondition)
		fieldsResponseFields := r.NewResponseFields(path, &selection.SelectionSet)
		typ := r.namedType(path, func() types.Type {
			return r.genFromResponseFields(path, fieldsResponseFields)
		})
		return &ResponseField{
			Name:             selection.TypeCondition,
			Type:             typ,
			IsInlineFragment: true,
			ResponseFields:   fieldsResponseFields,
			Tags:             []string{`json:"-"`},
		}
	}

	panic("unexpected selection type")
}

func (r *SourceGenerator) OperationArguments(variableDefinitions ast.VariableDefinitionList) []*Argument {
	argumentTypes := make([]*Argument, 0, len(variableDefinitions))
	for _, v := range variableDefinitions {
		baseType := r.namedType(NewFieldPath(v.Definition.Kind, v.Definition.Name), func() types.Type {
			return r.genFromDefinition(v.Definition)
		})

		typ := r.binder.CopyModifiersFromAst(v.Type, baseType)

		argumentTypes = append(argumentTypes, &Argument{
			Variable: v.Variable,
			Type:     typ,
		})
	}

	return argumentTypes
}
