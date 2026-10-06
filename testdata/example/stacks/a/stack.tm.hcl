stack {
  tags  = ["prod", "net"]
  after = ["/stacks/b", "../c", "/stacks/nope", "/imports", "tag:prod:net", "tag:prood", "tag:dev:net"]
  wants = ["/globals.tm.hcl"]
  before = ["."]
}

import {
  source = "/imports/common.tm.hcl"
}

generate_hcl "main.tf" {
  content {
    tags    = global.tags
    region  = global.region
    cidr    = global.net.cidr
    x       = "${global.from_import}-${global.sibling_only}"
  }
}
