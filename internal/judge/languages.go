package judge

type Language struct {
	Image      string
	SourceFile string
	Compile    []string
	Run        []string
}

func Languages(cpp, python, goImage string) map[string]Language {
	return map[string]Language{
		"cpp20":   {cpp, "main.cpp", []string{"g++", "-std=c++20", "-O2", "-pipe", "-DONLINE_JUDGE", "/work/main.cpp", "-o", "/tmp/program"}, []string{"/work/program"}},
		"python3": {python, "main.py", []string{"python3", "-I", "-c", "import ast; ast.parse(open('/work/main.py').read())"}, []string{"python3", "-I", "-B", "/work/main.py"}},
		"go":      {goImage, "main.go", []string{"go", "build", "-trimpath", "-o", "/tmp/program", "/work/main.go"}, []string{"/work/program"}},
	}
}
