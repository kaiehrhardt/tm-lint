stack {
  tags   = ["dev"]
  before = ["/stacks", "tag:~dev"]
}

globals {
  sibling_only = "b"
  b_used       = 2
  child_val    = 3
}

generate_file "f.txt" {
  lets {
    a = 1
  }
  content = "${global.b_used}-${let.a}"
}
