//go:generate daggen -type=ConstOp -output=math_const_gen.go
//go:generate daggen -type=AddOp -output=math_add_gen.go
//go:generate daggen -type=SubOp -output=math_sub_gen.go
//go:generate daggen -type=DivOp -output=math_div_gen.go
//go:generate daggen -type=PackMathOperandsOp -output=math_pack_gen.go
//go:generate daggen -type=StringConstOp -output=string_const_gen.go
//go:generate daggen -type=StringLookupOp -output=string_lookup_gen.go
//go:generate daggen -type=StringToLowerOp -output=string_tolower_gen.go
//go:generate daggen -type=CityTimeOp -output=time_city_gen.go
//go:generate daggen -type=ModeSelectOp -output=string_modeselect_gen.go
package library
