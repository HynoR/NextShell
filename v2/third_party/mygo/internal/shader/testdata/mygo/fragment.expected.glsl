uniform vec2 U0[4];
uniform vec2 U1;
uniform float U2;
uniform vec2 U3;
uniform vec3 U4;
uniform vec3 U5;
uniform vec4 U6;
uniform uint U7;
uniform int U8;
uniform vec4 U9[4];
uniform sampler2D T0;
uniform sampler2D T1;
uniform sampler2D T2;
uniform sampler2D T3;
uniform ivec4 flipRowsOf;

ivec2 flipRows(ivec2 p, int h) {
	return h > 0 ? ivec2(p.x, h - 1 - p.y) : p;
}

uint F0(in uint l0);
float F2(void);
vec4 F10(in vec2 l0);
vec4 F20(in vec4 l0, in vec2 l1);

uint F0(in uint l0) {
	uint l1 = 0u;
	uint l2 = 0u;
	l1 = ((l0) * (747796405u)) + (2891336453u);
	l2 = (((l1) >> (((l1) >> (28)) + (4u))) ^ (l1)) * (277803737u);
	return ((l2) >> (22)) ^ (l2);
}

float F2(void) {
	return U2;
}

vec4 F10(in vec2 l0) {
	vec2 l1 = vec2(0);
	vec2 l2 = vec2(0);
	vec2 l3 = vec2(0);
	vec2 l4 = vec2(0);
	vec4 l5 = vec4(0);
	vec4 l6 = vec4(0);
	vec4 l7 = vec4(0);
	vec4 l8 = vec4(0);
	l1 = (l0) - (5.0000000000e-01);
	l2 = floor(l1);
	l3 = (l1) - (l2);
	l4 = ((U0)[0]) - (1.0);
	l5 = texelFetch(T0, flipRows(ivec2(floor(clamp(l2, vec2(0.0), l4))), flipRowsOf[0]), 0);
	l6 = texelFetch(T0, flipRows(ivec2(floor(clamp((l2) + (vec2(1.0, 0.0)), vec2(0.0), l4))), flipRowsOf[0]), 0);
	l7 = texelFetch(T0, flipRows(ivec2(floor(clamp((l2) + (vec2(0.0, 1.0)), vec2(0.0), l4))), flipRowsOf[0]), 0);
	l8 = texelFetch(T0, flipRows(ivec2(floor(clamp((l2) + (vec2(1.0)), vec2(0.0), l4))), flipRowsOf[0]), 0);
	return mix(mix(l5, l6, (l3).x), mix(l7, l8, (l3).x), (l3).y);
}

vec4 F20(in vec4 l0, in vec2 l1) {
	float l2 = float(0);
	uint l5 = 0u;
	l2 = 0.0;
	for (int l3 = 0; l3 < U8; l3++) {
		l2 = (l2) + ((((U9)[l3]).z) / ((1.0) + (distance(l1, ((U9)[l3]).xy))));
	}
	for (uint l4 = 0u; l4 < U7; l4 += 2u) {
		l2 = (l2) + (1.0000000000e-01);
	}
	l5 = F0((((uvec3(uint(int((l0).x)))).z) * (2654435769u)) ^ (F0((uint(int((l0).y))) ^ (U7))));
	return (((F10((l1) * (F2()))) * (float((l5) % (255u)))) / (255.0)) * (l2);
}

vec4 shaderFragment(vec4 position, vec2 v0) {
	return F20(position, v0);
}
