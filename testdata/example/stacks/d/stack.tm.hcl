stack {}

generate_hcl "d.tf" {
  lets {
    used   = 1
    unused = 2
    chain  = let.used
    map "m" {
      for_each = ["a"]
      key      = element.new
      value {
        v = element.new
      }
    }
  }

  content {
    v      = global.dyn[terramate.stack.name]
    env    = global["env"]
    c      = let.chain
    region = tm_try(global.regoin, "eu-west-1")
    y      = global.missing
    typo   = tm_try(global.net.cidrr, "10.0.0.0/8")
    opt    = tm_try(global.net.optional_flag, false)
    tagz   = global.tagz.team
    # tm-lint:ignore undefined-global
    z      = global.external_only
  }
}
