package utils

import "hash/fnv"

func HashAndModulo(str string, modulo int) int {
	hasher := fnv.New32a()
	hasher.Write([]byte(str))
	hash := hasher.Sum32()
	return int(hash) % modulo
}
