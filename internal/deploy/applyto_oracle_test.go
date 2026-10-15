package deploy

import "testing"

// applyToOracleCases are instruction files with the .claude/rules/<name>.md
// (claude) and the applyTo that compile groups by (applyTo) that Oracle
// 0.33.0 gives them: InstructionIntegrator._convert_to_claude_rules and
// parse_primitive_file, run on each content. err marks a frontmatter the
// Oracle refuses to read (install exits 1, compile skips the file).
var applyToOracleCases = []struct {
	name, in, claude, applyTo string
	err                       bool
}{
	{"block list", "---\napplyTo:\n  - \"src/**/*.ts\"\n  - \"web/**/*.ts\"\ndescription: list\n---\nbody\n", "---\npaths:\n  - \"src/**/*.ts\"\n  - \"web/**/*.ts\"\n---\n\nbody\n", "src/**/*.ts,web/**/*.ts", false},
	{"block list one item", "---\napplyTo:\n  - \"only/**\"\n---\nbody\n", "---\npaths:\n  - \"only/**\"\n---\n\nbody\n", "only/**", false},
	{"block list at column 0", "---\napplyTo:\n- \"z/**\"\n- \"w/**\"\ndescription: x\n---\nbody\n", "---\npaths:\n  - \"z/**\"\n  - \"w/**\"\n---\n\nbody\n", "z/**,w/**", false},
	{"flow list", "---\napplyTo: [\"x/**\", \"y/**\"]\n---\nbody\n", "---\npaths:\n  - \"x/**\"\n  - \"y/**\"\n---\n\nbody\n", "x/**,y/**", false},
	{"empty flow list", "---\napplyTo: []\n---\nbody\n", "body\n", "", false},
	{"null", "---\napplyTo: null\n---\nbody\n", "body\n", "", false},
	{"empty string", "---\napplyTo: \"\"\n---\nbody\n", "body\n", "", false},
	{"list with empty item", "---\napplyTo:\n  - \"a/**\"\n  - \"\"\n  - \"b/**\"\n---\nbody\n", "---\npaths:\n  - \"a/**\"\n  - \"b/**\"\n---\n\nbody\n", "a/**,b/**", false},
	{"list item with comma", "---\napplyTo:\n  - \"c/**, d/**\"\n---\nbody\n", "---\npaths:\n  - \"c/**, d/**\"\n---\n\nbody\n", "c/**\\, d/**", false},
	{"list with int", "---\napplyTo:\n  - 1\n  - \"n/**\"\n---\nbody\n", "---\npaths:\n  - \"1\"\n  - \"n/**\"\n---\n\nbody\n", "1,n/**", false},
	{"list with nested list", "---\napplyTo:\n  - \"t/**\"\n  - [\"u/**\", \"v/**\"]\n---\nbody\n", "---\npaths:\n  - \"t/**\"\n  - \"['u/**', 'v/**']\"\n---\n\nbody\n", "t/**,['u/**', 'v/**']", false},
	{"list with bool, nested list, mapping", "---\napplyTo:\n  - yes\n  - [x, 2, null, \"it's\"]\n  - {k: v}\n---\nbody\n", "---\npaths:\n  - \"True\"\n  - \"['x', 2, None, \\\"it's\\\"]\"\n  - \"{'k': 'v'}\"\n---\n\nbody\n", "True,['x', 2, None, \"it's\"],{'k': 'v'}", false},
	{"crlf block list", "---\r\napplyTo:\r\n  - \"r/**\"\r\n  - \"s/**\"\r\n---\r\nbody\r\n", "---\npaths:\n  - \"r/**\"\n  - \"s/**\"\n---\n\nbody\r\n", "r/**,s/**", false},
	{"true", "---\napplyTo: true\n---\nbody\n", "---\npaths:\n  - \"True\"\n---\n\nbody\n", "True", false},
	{"yes", "---\napplyTo: yes\n---\nbody\n", "---\npaths:\n  - \"True\"\n---\n\nbody\n", "True", false},
	{"float 1.50", "---\napplyTo: 1.50\n---\nbody\n", "---\npaths:\n  - \"1.5\"\n---\n\nbody\n", "1.5", false},
	{"1e3 is a string", "---\napplyTo: 1e3\n---\nbody\n", "---\npaths:\n  - \"1e3\"\n---\n\nbody\n", "1e3", false},
	{"0o17 is a string", "---\napplyTo: 0o17\n---\nbody\n", "---\npaths:\n  - \"0o17\"\n---\n\nbody\n", "0o17", false},
	{"date", "---\napplyTo: 2024-01-02\n---\nbody\n", "---\npaths:\n  - \"2024-01-02\"\n---\n\nbody\n", "2024-01-02", false},
	{"flow mapping", "---\napplyTo: {a: b}\n---\nbody\n", "---\npaths:\n  - \"{'a': 'b'}\"\n---\n\nbody\n", "{'a': 'b'}", false},
	{"trailing comment", "---\napplyTo: \"q/**\"  # trailing\n---\nbody\n", "---\npaths:\n  - \"q/**\"\n---\n\nbody\n", "q/**", false},
	{"single-quote escape", "---\napplyTo: 'it''s/**'\n---\nbody\n", "---\npaths:\n  - \"it's/**\"\n---\n\nbody\n", "it's/**", false},
	{"folded", "---\napplyTo: >-\n  f/**,\n  g/**\n---\nbody\n", "---\npaths:\n  - \"f/**\"\n  - \"g/**\"\n---\n\nbody\n", "f/**, g/**", false},
	{"escaped comma", "---\napplyTo: \"a/**\\\\, b/**\"\n---\nbody\n", "---\npaths:\n  - \"a/**, b/**\"\n---\n\nbody\n", "a/**\\, b/**", false},
	{"escaped backslash", "---\napplyTo: \"a\\\\b,c\"\n---\nbody\n", "---\npaths:\n  - \"a\\\\b\"\n  - \"c\"\n---\n\nbody\n", "a\\b,c", false},
	{"comma in character class", "---\napplyTo: \"src/[a,b]/*.ts,x/**\"\n---\nbody\n", "---\npaths:\n  - \"src/[a,b]/*.ts\"\n  - \"x/**\"\n---\n\nbody\n", "src/[a,b]/*.ts,x/**", false},
	{"non-ASCII and YAML tab escape", "---\napplyTo: \"src/日本/**, a\\tb\"\n---\nbody\n", "---\npaths:\n  - \"src/\\u65e5\\u672c/**\"\n  - \"a\\tb\"\n---\n\nbody\n", "src/日本/**, a\tb", false},
	{"hex int", "---\napplyTo: 0x1F\n---\nbody\n", "---\npaths:\n  - \"31\"\n---\n\nbody\n", "31", false},
	{"old octal int", "---\napplyTo: 017\n---\nbody\n", "---\npaths:\n  - \"15\"\n---\n\nbody\n", "15", false},
	{"int with underscore", "---\napplyTo: 1_000\n---\nbody\n", "---\npaths:\n  - \"1000\"\n---\n\nbody\n", "1000", false},
	{"sexagesimal int", "---\napplyTo: 1:30\n---\nbody\n", "---\npaths:\n  - \"90\"\n---\n\nbody\n", "90", false},
	{"datetime", "---\napplyTo: 2024-01-02 10:00:00\n---\nbody\n", "---\npaths:\n  - \"2024-01-02 10:00:00\"\n---\n\nbody\n", "2024-01-02 10:00:00", false},
	{"datetime utc", "---\napplyTo: 2024-01-02T10:00:00Z\n---\nbody\n", "---\npaths:\n  - \"2024-01-02 10:00:00+00:00\"\n---\n\nbody\n", "2024-01-02 10:00:00+00:00", false},
	{"datetime offset fraction", "---\napplyTo: 2001-12-14t21:59:43.10-05:00\n---\nbody\n", "---\npaths:\n  - \"2001-12-14 21:59:43.100000-05:00\"\n---\n\nbody\n", "2001-12-14 21:59:43.100000-05:00", false},
	{"datetime one-digit fields", "---\napplyTo: 2024-1-2 3:04:05\n---\nbody\n", "---\npaths:\n  - \"2024-01-02 03:04:05\"\n---\n\nbody\n", "2024-01-02 03:04:05", false},
	{"inf", "---\napplyTo: .inf\n---\nbody\n", "---\npaths:\n  - \"inf\"\n---\n\nbody\n", "inf", false},
	{"negative inf", "---\napplyTo: -.inf\n---\nbody\n", "---\npaths:\n  - \"-inf\"\n---\n\nbody\n", "-inf", false},
	{"nan", "---\napplyTo: .nan\n---\nbody\n", "---\npaths:\n  - \"nan\"\n---\n\nbody\n", "nan", false},
	{"float with exponent", "---\napplyTo: 1.0e+3\n---\nbody\n", "---\npaths:\n  - \"1000.0\"\n---\n\nbody\n", "1000.0", false},
	{"sexagesimal float", "---\napplyTo: 190:20:30.15\n---\nbody\n", "---\npaths:\n  - \"685230.15\"\n---\n\nbody\n", "685230.15", false},
	{"binary int", "---\napplyTo: 0b101\n---\nbody\n", "---\npaths:\n  - \"5\"\n---\n\nbody\n", "5", false},
	{"negative zero int", "---\napplyTo: -0\n---\nbody\n", "---\npaths:\n  - \"0\"\n---\n\nbody\n", "0", false},
	{"1e-7 is a string", "---\napplyTo: 1e-7\n---\nbody\n", "---\npaths:\n  - \"1e-7\"\n---\n\nbody\n", "1e-7", false},
	{"float repr 1e+20", "---\napplyTo: 100000000000000000000.0\n---\nbody\n", "---\npaths:\n  - \"1e+20\"\n---\n\nbody\n", "1e+20", false},
	{"float 1.", "---\napplyTo: 1.\n---\nbody\n", "---\npaths:\n  - \"1.0\"\n---\n\nbody\n", "1.0", false},
	{"big int", "---\napplyTo: 12345678901234567890\n---\nbody\n", "---\npaths:\n  - \"12345678901234567890\"\n---\n\nbody\n", "12345678901234567890", false},
	{"tilde", "---\napplyTo: ~\n---\nbody\n", "body\n", "", false},
	{"On", "---\napplyTo: On\n---\nbody\n", "---\npaths:\n  - \"True\"\n---\n\nbody\n", "True", false},
	{"OFF", "---\napplyTo: OFF\n---\nbody\n", "---\npaths:\n  - \"False\"\n---\n\nbody\n", "False", false},
	{"y is a string", "---\napplyTo: y\n---\nbody\n", "---\npaths:\n  - \"y\"\n---\n\nbody\n", "y", false},
	{"explicit str tag", "---\napplyTo: !!str 1\n---\nbody\n", "---\npaths:\n  - \"1\"\n---\n\nbody\n", "1", false},
	{"list of scalars", "---\napplyTo: [2024-01-02, 2024-01-02 10:00:00, .inf, 0x1F, 1.0]\n---\nbody\n", "---\npaths:\n  - \"2024-01-02\"\n  - \"2024-01-02 10:00:00\"\n  - \"inf\"\n  - \"31\"\n  - \"1.0\"\n---\n\nbody\n", "2024-01-02,2024-01-02 10:00:00,inf,31,1.0", false},
	{"repr of dates and datetimes", "---\napplyTo: [[2024-01-02, 2024-01-02 10:00:00.5, 2024-01-02 10:00:05, 2024-01-02T10:00:00Z, 2024-01-02T10:00:00-05:00, 2024-01-02T10:00:00+05:30, 2024-01-02 10:00:00]]\n---\nbody\n", "---\npaths:\n  - \"[datetime.date(2024, 1, 2), datetime.datetime(2024, 1, 2, 10, 0, 0, 500000), datetime.datetime(2024, 1, 2, 10, 0, 5), datetime.datetime(2024, 1, 2, 10, 0, tzinfo=datetime.timezone.utc), datetime.datetime(2024, 1, 2, 10, 0, tzinfo=datetime.timezone(datetime.timedelta(days=-1, seconds=68400))), datetime.datetime(2024, 1, 2, 10, 0, tzinfo=datetime.timezone(datetime.timedelta(seconds=19800))), datetime.datetime(2024, 1, 2, 10, 0)]\"\n---\n\nbody\n", "[datetime.date(2024, 1, 2), datetime.datetime(2024, 1, 2, 10, 0, 0, 500000), datetime.datetime(2024, 1, 2, 10, 0, 5), datetime.datetime(2024, 1, 2, 10, 0, tzinfo=datetime.timezone.utc), datetime.datetime(2024, 1, 2, 10, 0, tzinfo=datetime.timezone(datetime.timedelta(days=-1, seconds=68400))), datetime.datetime(2024, 1, 2, 10, 0, tzinfo=datetime.timezone(datetime.timedelta(seconds=19800))), datetime.datetime(2024, 1, 2, 10, 0)]", false},
	{"repr of floats", "---\napplyTo: [[.inf, -.inf, .nan, -0.0, 0.0001, 0.00001, 1.0e+16, 1.0e+15, 1.5e-7, 123456789.123456789, 1:30.5]]\n---\nbody\n", "---\npaths:\n  - \"[inf, -inf, nan, -0.0, 0.0001, 1e-05, 1e+16, 1000000000000000.0, 1.5e-07, 123456789.12345679, 90.5]\"\n---\n\nbody\n", "[inf, -inf, nan, -0.0, 0.0001, 1e-05, 1e+16, 1000000000000000.0, 1.5e-07, 123456789.12345679, 90.5]", false},
	{"repr of ints", "---\napplyTo: [[1:30, 0b11, 017, 1_0, -0x1F]]\n---\nbody\n", "---\npaths:\n  - \"[90, 3, 15, 10, -31]\"\n---\n\nbody\n", "[90, 3, 15, 10, -31]", false},
	{"mapping keys equal across numeric types", "---\napplyTo: {1: a, true: b, 1.0: c, x: [1]}\n---\nbody\n", "---\npaths:\n  - \"{1: 'c', 'x': [1]}\"\n---\n\nbody\n", "{1: 'c', 'x': [1]}", false},
	{"equal positive infinity keys", "---\napplyTo: [{.inf: a, .inf: b}]\n---\nbody\n", "---\npaths:\n  - \"{inf: 'b'}\"\n---\n\nbody\n", "{inf: 'b'}", false},
	{"infinity keys of opposite sign", "---\napplyTo: [{.inf: a, -.inf: b}]\n---\nbody\n", "---\npaths:\n  - \"{inf: 'a', -inf: 'b'}\"\n---\n\nbody\n", "{inf: 'a', -inf: 'b'}", false},
	{"equal negative infinity keys", "---\napplyTo: [{-.inf: a, -.inf: b}]\n---\nbody\n", "---\npaths:\n  - \"{-inf: 'b'}\"\n---\n\nbody\n", "{-inf: 'b'}", false},
	{"NaN keys are one key", "---\napplyTo: [{.nan: a, .nan: b}]\n---\nbody\n", "---\npaths:\n  - \"{nan: 'b'}\"\n---\n\nbody\n", "{nan: 'b'}", false},
	{"mapping with null and empty keys", "---\napplyTo: [{null: 1, '': 2}]\n---\nbody\n", "---\npaths:\n  - \"{None: 1, '': 2}\"\n---\n\nbody\n", "{None: 1, '': 2}", false},
	{"list items with quotes and backslash", "---\napplyTo: [\"a'b\", 'a\"b', \"a'\\\"b\", 'x\\y']\n---\nbody\n", "---\npaths:\n  - \"a'b\"\n  - \"a\\\"b\"\n  - \"a'\\\"b\"\n  - \"x\\\\y\"\n---\n\nbody\n", "a'b,a\"b,a'\"b,x\\\\y", false},
	{"list items stripped of Python whitespace", "---\napplyTo: [\" \\x1cq/**\\x1c\", \"\\u00a0p/**\", \"\\u2028x\"]\n---\nbody\n", "---\npaths:\n  - \"q/**\"\n  - \"p/**\"\n  - \"x\"\n---\n\nbody\n", "q/**,p/**,x", false},
	{"scalar not stripped", "---\napplyTo: \"\\x1cq/** \"\n---\nbody\n", "---\npaths:\n  - \"q/**\"\n---\n\nbody\n", "\x1cq/** ", false},
	{"scalar escaped comma alone", "---\napplyTo: \"\\\\,x\"\n---\nbody\n", "---\npaths:\n  - \",x\"\n---\n\nbody\n", "\\,x", false},
	{"escaped comma inside character class", "---\napplyTo: \"[a\\\\,b],c\"\n---\nbody\n", "---\npaths:\n  - \"[a,b]\"\n  - \"c\"\n---\n\nbody\n", "[a\\,b],c", false},
	{"trailing backslash", "---\napplyTo: \"{a,b}\\\\\"\n---\nbody\n", "---\npaths:\n  - \"{a,b}\\\\\"\n---\n\nbody\n", "{a,b}\\", false},
	{"list items escaped for the separator", "---\napplyTo: [\"a\\\\b\", \"[x,y]\", \"{p,q}\", \"r,s\"]\n---\nbody\n", "---\npaths:\n  - \"a\\\\b\"\n  - \"[x,y]\"\n  - \"{p,q}\"\n  - \"r,s\"\n---\n\nbody\n", "a\\\\b,[x,y],{p,q},r\\,s", false},
	{"empty containers in list", "---\napplyTo: [[], [{}], [[1, [2]]]]\n---\nbody\n", "---\npaths:\n  - \"[]\"\n  - \"[{}]\"\n  - \"[[1, [2]]]\"\n---\n\nbody\n", "[],[{}],[[1, [2]]]", false},
	{"NUL", "---\napplyTo: \"\\x00\"\n---\nbody\n", "---\npaths:\n  - \"\\u0000\"\n---\n\nbody\n", "\x00", false},
	{"repr of non-printable characters", "---\napplyTo: [[\"\\x01\\x7f\\x80\\xa0\\xad\", \"\\u200b\\ufeff\", \"\\U0001F600\\U000E0001\", \"\\t\\n\\r\\\\\", \"it's\", \"a'\\\"b\"]]\n---\nbody\n", "---\npaths:\n  - \"['\\\\x01\\\\x7f\\\\x80\\\\xa0\\\\xad', '\\\\u200b\\\\ufeff', '\\ud83d\\ude00\\\\U000e0001', '\\\\t\\\\n\\\\r\\\\', \\\"it's\\\", 'a\\\\'\\\"b']\"\n---\n\nbody\n", "['\\x01\\x7f\\x80\\xa0\\xad', '\\u200b\\ufeff', '😀\\U000e0001', '\\t\\n\\r\\\\', \"it's\", 'a\\'\"b']", false},
	{"list of nulls and an int", "---\napplyTo: [1, null, ~, '']\n---\nbody\n", "---\npaths:\n  - \"1\"\n---\n\nbody\n", "1", false},
	{"astral character", "---\napplyTo: \"\\u00e9\\U0001F600\"\n---\nbody\n", "---\npaths:\n  - \"\\u00e9\\ud83d\\ude00\"\n---\n\nbody\n", "é😀", false},
	{"JSON control escapes", "---\napplyTo: \"a\\bb\\fc\\x1fd\\x7fe/f\"\n---\nbody\n", "---\npaths:\n  - \"a\\bb\\fc\\u001fd\\u007fe/f\"\n---\n\nbody\n", "a\x08b\x0cc\x1fd\x7fe/f", false},
	{"literal block", "---\napplyTo: |\n  a/**\n  b/**\n---\nbody\n", "---\npaths:\n  - \"a/**\\nb/**\"\n---\n\nbody\n", "a/**\nb/**\n", false},
	{"empty literal block", "---\napplyTo: |\ndescription: d\n---\nbody\n", "body\n", "", false},
	{"bare scalar alias", "---\napplyTo: **/*.go\n---\nbody\n", "", "", true},
	{"unquoted comma list", "---\napplyTo: **/src/**,**/api/**\n---\nbody\n", "", "", true},
	{"unquoted brace comma list", "---\napplyTo: \"**/*.{css,scss},**/*.py\"\n---\nbody\n", "---\npaths:\n  - \"**/*.{css,scss}\"\n  - \"**/*.py\"\n---\n\nbody\n", "**/*.{css,scss},**/*.py", false},
	{"unmatched closing brace", "---\napplyTo: a}b,{c,d}\n---\nbody\n", "---\npaths:\n  - \"a}b\"\n  - \"{c,d}\"\n---\n\nbody\n", "a}b,{c,d}", false},
	{"mixed surrounding quotes", "---\napplyTo: \"'**/*.go'\"\n---\nbody\n", "---\npaths:\n  - \"'**/*.go'\"\n---\n\nbody\n", "'**/*.go'", false},
	{"leading comma", "---\napplyTo: , **/a ,, **/b ,\n---\nbody\n", "", "", true},
	{"empty segments quoted", "---\napplyTo: \", **/a ,, **/b ,\"\n---\nbody\n", "---\npaths:\n  - \"**/a\"\n  - \"**/b\"\n---\n\nbody\n", ", **/a ,, **/b ,", false},
	{"repeated key", "---\napplyTo: \"**/*.go\"\napplyTo: \"**/*.py\"\n---\nbody\n", "---\npaths:\n  - \"**/*.py\"\n---\n\nbody\n", "**/*.py", false},
	{"indented mapping", "---\n  applyTo: \"**/*.md\"\n---\nbody\n", "---\npaths:\n  - \"**/*.md\"\n---\n\nbody\n", "**/*.md", false},
	{"plain scalar with quote and backslash", "---\napplyTo: a\"b\\c\n---\nbody\n", "---\npaths:\n  - \"a\\\"b\\\\c\"\n---\n\nbody\n", "a\"b\\c", false},
	{"empty value", "---\napplyTo:\n---\nbody\n", "body\n", "", false},
	{"whitespace value", "---\napplyTo:   \n---\nbody\n", "body\n", "", false},
	{"no applyTo", "---\ntitle: X\ndescription: Y\n---\n\n# Body\n", "# Body\n", "", false},
	{"flow list leading null", "---\napplyTo: [null, '**/*.py']\n---\nbody\n", "---\npaths:\n  - \"**/*.py\"\n---\n\nbody\n", "**/*.py", false},
	{"flow list leading tilde", "---\napplyTo: [~, '**/*.py']\n---\nbody\n", "---\npaths:\n  - \"**/*.py\"\n---\n\nbody\n", "**/*.py", false},
	{"block list leading empty item", "---\napplyTo:\n  -\n  - \"**/*.py\"\n---\nbody\n", "---\npaths:\n  - \"**/*.py\"\n---\n\nbody\n", "**/*.py", false},
	{"flow list of nulls", "---\napplyTo: [null, ~]\n---\nbody\n", "body\n", "", false},
	{"flow list single quotes", "---\napplyTo: ['**/*.py', '**/*.rb']\n---\nbody\n", "---\npaths:\n  - \"**/*.py\"\n  - \"**/*.rb\"\n---\n\nbody\n", "**/*.py,**/*.rb", false},
	{"sequence document", "---\n- x\n---\nbody\n", "body\n", "", false},
	{"comment only", "---\n# nothing\n---\nbody\n", "body\n", "", false},
	// Deviation: the Oracle reads this frontmatter; the OpenAPM safe subset refuses it.
	{"anchor and alias", "---\na: &x \"k/**\"\napplyTo: *x\n---\nbody\n", "", "", true},
	{"custom tag", "---\napplyTo: !foo \"k/**\"\n---\nbody\n", "", "", true},
	{"unclosed flow", "---\nfoo: [unclosed\napplyTo: \"k/**\"\n---\nbody\n", "", "", true},
	// Deviation: the Oracle reads this frontmatter; the OpenAPM safe subset refuses it.
	{"merge key", "---\n<<: {applyTo: \"m/**\"}\n---\nbody\n", "", "", true},
	// Deviation: the Oracle converts these explicit tags (1.0, b'hello', a
	// set in hash order, a list of tuples); apm-go refuses a tag the YAML 1.1
	// resolver would not give the value.
	{"explicit float tag on an int", "---\napplyTo: !!float 1\n---\nbody\n", "", "", true},
	{"binary tag", "---\napplyTo: !!binary aGVsbG8=\n---\nbody\n", "", "", true},
	{"set tag", "---\napplyTo: !!set {a, b}\n---\nbody\n", "", "", true},
	{"omap tag", "---\napplyTo: !!omap [a: 1]\n---\nbody\n", "", "", true},
	{"invalid date", "---\napplyTo: 2024-13-01\n---\nbody\n", "", "", true},
	{"value tag", "---\napplyTo: =\n---\nbody\n", "", "", true},
	{"unhashable key", "---\n[a]: 1\napplyTo: \"k/**\"\n---\nbody\n", "", "", true},
}

func TestConvertToClaudeRules_OracleApplyTo(t *testing.T) {
	for _, tt := range applyToOracleCases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertToClaudeRules([]byte(tt.in))
			if tt.err {
				if err == nil {
					t.Fatalf("convertToClaudeRules(%q) = %q, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("convertToClaudeRules(%q): %v", tt.in, err)
			}
			if string(got) != tt.claude {
				t.Errorf("convertToClaudeRules(%q)\n got: %q\nwant: %q", tt.in, got, tt.claude)
			}
		})
	}
}

func TestReadInstruction_OracleApplyTo(t *testing.T) {
	for _, tt := range applyToOracleCases {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := ReadInstruction([]byte(tt.in))
			if tt.err {
				if err == nil {
					t.Fatalf("ReadInstruction(%q) applyTo = %q, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadInstruction(%q): %v", tt.in, err)
			}
			if got != tt.applyTo {
				t.Errorf("ReadInstruction(%q) applyTo = %q, want %q", tt.in, got, tt.applyTo)
			}
		})
	}
}
