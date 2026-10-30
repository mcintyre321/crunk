# Fan out with a parallel block.
#
# Everything inside `parallel "greeting"` is repeated once per element of
# for_each. The host block gives each repetition its own logical host.
# greeting.index is 0..2, greeting.key and greeting.value are the words.

parallel "greeting" {
  for_each = ["Hello", "Bonjour", "Hola"]

  host "worker" {
    container "say" {
      image  = "docker.io/library/alpine:3.20"
      stdin  = jsonencode({ word = greeting.value, position = greeting.index })
      script = "cat"
    }
  }
}
