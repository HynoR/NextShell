package main

var Seed uint
var Count int
var Taps [4]vec4

func pcg(v uint) uint {
	s := v*747796405 + 2891336453
	w := ((s >> ((s >> 28) + 4)) ^ s) * 277803737
	return (w >> 22) ^ w
}

func Fragment(dst vec4, pos vec2) vec4 {
	glow := 0.0
	for i := 0; i < Count; i++ {
		glow += Taps[i].z / (1 + distance(pos, Taps[i].xy))
	}
	for j := uint(0); j < Seed; j += 2 {
		glow += 0.1
	}
	h := pcg(uvec3(uint(int(dst.x))).z*0x9E3779B9 ^ pcg(uint(int(dst.y))^Seed))
	return image0Sample(pos*pixelScale()) * float(h%255) / 255 * glow
}
