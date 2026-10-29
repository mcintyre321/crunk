# Fan out over a dimension.
#
# Each `say` instance runs on its own logical host (host.worker is expanded over
# dimension.greeting), so `say` is expanded too. each.index is 0..2 and
# each.key is "0".."2".

locals {
  greetings = ["Hello", "Bonjour", "Hola"]
}

dimension "greeting" { size = length(local.greetings) }

host "worker" { for_each = dimension.greeting }

container "say" {
  host   = host.worker
  image  = "docker.io/library/alpine:3.20"
  input  = { word = local.greetings[each.index] }
  script = "cat /run/input.json"
}
