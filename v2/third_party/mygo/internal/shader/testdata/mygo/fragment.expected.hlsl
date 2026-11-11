// mod and modInt are macros rather than functions. Some HLSL compilers cannot
// resolve overloads on the vector size, as a scalar argument is implicitly convertible
// to any vector type (#3356).

// mod is a floored modulo like GLSL's mod. HLSL's fmod is a truncated modulo.
#define mod(x, y) ((x) - (y) * floor((x) / (y)))

// modInt is a truncated modulo like the '%' operator. fxc emits warning X3556 for signed
// integer % and /, and suggests uints, so the magnitude is computed with unsigned modulo
// and the sign of the dividend is reapplied. This matches '%' except for the minimum
// integer, where abs overflows.
#define modInt(x, y) (sign(x) * asint(asuint(abs(x)) % asuint(abs(y))))

float2x2 float2x2FromScalar(float x) {
	return float2x2(x, 0, 0, x);
}

float3x3 float3x3FromScalar(float x) {
	return float3x3(x, 0, 0, 0, x, 0, 0, 0, x);
}

float4x4 float4x4FromScalar(float x) {
	return float4x4(x, 0, 0, 0, 0, x, 0, 0, 0, 0, x, 0, 0, 0, 0, x);
}

cbuffer Uniforms : register(b2) {
	float2 U0[4] : packoffset(c0);
	float2 U1 : packoffset(c3.z);
	float U2 : packoffset(c4);
	float2 U3 : packoffset(c4.y);
	float3 U4 : packoffset(c5);
	float3 U5 : packoffset(c6);
	float4 U6 : packoffset(c7);
	uint U7 : packoffset(c8);
	int U8 : packoffset(c8.y);
	float4 U9[4] : packoffset(c9);
}

Texture2D T0 : register(t6);
Texture2D T1 : register(t7);
Texture2D T2 : register(t8);
Texture2D T3 : register(t9);

uint F0(in uint l0);
float F2(void);
float4 F10(in float2 l0);
float4 F20(in float4 l0, in float2 l1);

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

float4 F10(in float2 l0) {
	float2 l1 = 0.0;
	float2 l2 = 0.0;
	float2 l3 = 0.0;
	float2 l4 = 0.0;
	float4 l5 = 0.0;
	float4 l6 = 0.0;
	float4 l7 = 0.0;
	float4 l8 = 0.0;
	l1 = (l0) - (5.0000000000e-01);
	l2 = floor(l1);
	l3 = (l1) - (l2);
	l4 = ((U0)[0]) - (1.0);
	l5 = T0.Load(int3(int2(floor(clamp(l2, (float2)(0.0), l4))), 0));
	l6 = T0.Load(int3(int2(floor(clamp((l2) + (float2(1.0, 0.0)), (float2)(0.0), l4))), 0));
	l7 = T0.Load(int3(int2(floor(clamp((l2) + (float2(0.0, 1.0)), (float2)(0.0), l4))), 0));
	l8 = T0.Load(int3(int2(floor(clamp((l2) + ((float2)(1.0)), (float2)(0.0), l4))), 0));
	return lerp(lerp(l5, l6, (l3).x), lerp(l7, l8, (l3).x), (l3).y);
}

float4 F20(in float4 l0, in float2 l1) {
	float l2 = 0.0;
	uint l5 = 0u;
	l2 = 0.0;
	for (int l3 = 0; l3 < U8; l3++) {
		l2 = (l2) + ((((U9)[l3]).z) / ((1.0) + (distance(l1, ((U9)[l3]).xy))));
	}
	for (uint l4 = 0u; l4 < U7; l4 += 2u) {
		l2 = (l2) + (1.0000000000e-01);
	}
	l5 = F0(((((uint3)(uint(int((l0).x)))).z) * (2654435769u)) ^ (F0((uint(int((l0).y))) ^ (U7))));
	return (((F10((l1) * (F2()))) * (float((l5) % (255u)))) / (255.0)) * (l2);
}

float4 shaderFragment(float4 position, float2 v0) {
	return F20(position, v0);
}
