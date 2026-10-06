globals {
  region      = "eu-west-1"
  unused_root = 1
  env         = "prod"
}

globals "tags" {
  team = "platform"
  cost = "123"
}

globals "net" {
  cidr         = "10.0.0.0/16"
  unused_child = true
}

globals {
  # tm-lint:ignore unused-global
  ignored_one = 1
  ci_token    = "x"
}

globals "dyn" {
  a = 1
  b = 2
}

globals {
  map "users" {
    for_each = ["x"]
    key      = element.new
    value {
      name = element.new
    }
  }
}

generate_hcl "root.tf" {
  content {
    v = global.child_val
  }
}

globals {
  self_only = global.self_only
}
