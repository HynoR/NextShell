#include <metal_stdlib>

using namespace metal;

constexpr sampler __texelSampler(coord::pixel, filter::nearest, address::clamp_to_zero);

template<typename T, typename U>
T mod(T x, U y) {
	return x - y * floor(x/y);
}

// Metal defines sign only for floating-point types. These overloads cover int and ivecN arguments.
int sign(int x) {
	return int(x > 0) - int(x < 0);
}

int2 sign(int2 x) {
	return int2(x > 0) - int2(x < 0);
}

int3 sign(int3 x) {
	return int3(x > 0) - int3(x < 0);
}

int4 sign(int4 x) {
	return int4(x > 0) - int4(x < 0);
}

struct Uniforms {
	array<float2, 4> U0;
	float2 U1;
	float U2;
	float2 U3;
	float3 U4;
	float3 U5;
	float4 U6;
	uint U7;
	int U8;
	array<float4, 4> U9;
};

uint F0(constant Uniforms& uniforms, texture2d<float> T0, texture2d<float> T1, texture2d<float> T2, texture2d<float> T3, uint l0);
float F2(constant Uniforms& uniforms, texture2d<float> T0, texture2d<float> T1, texture2d<float> T2, texture2d<float> T3);
float4 F10(constant Uniforms& uniforms, texture2d<float> T0, texture2d<float> T1, texture2d<float> T2, texture2d<float> T3, float2 l0);
float4 F20(constant Uniforms& uniforms, texture2d<float> T0, texture2d<float> T1, texture2d<float> T2, texture2d<float> T3, float4 l0, float2 l1);

uint F0(constant Uniforms& uniforms, texture2d<float> T0, texture2d<float> T1, texture2d<float> T2, texture2d<float> T3, uint l0) {
	uint l1 = 0u;
	uint l2 = 0u;
	l1 = ((l0) * (747796405u)) + (2891336453u);
	l2 = (((l1) >> (((l1) >> (28)) + (4u))) ^ (l1)) * (277803737u);
	return ((l2) >> (22)) ^ (l2);
}

float F2(constant Uniforms& uniforms, texture2d<float> T0, texture2d<float> T1, texture2d<float> T2, texture2d<float> T3) {
	return uniforms.U2;
}

float4 F10(constant Uniforms& uniforms, texture2d<float> T0, texture2d<float> T1, texture2d<float> T2, texture2d<float> T3, float2 l0) {
	float2 l1 = float2(0);
	float2 l2 = float2(0);
	float2 l3 = float2(0);
	float2 l4 = float2(0);
	float4 l5 = float4(0);
	float4 l6 = float4(0);
	float4 l7 = float4(0);
	float4 l8 = float4(0);
	l1 = (l0) - (5.0000000000e-01);
	l2 = floor(l1);
	l3 = (l1) - (l2);
	l4 = ((uniforms.U0)[0]) - (1.0);
	l5 = T0.sample(__texelSampler, floor(clamp(l2, float2(0.0), l4)));
	l6 = T0.sample(__texelSampler, floor(clamp((l2) + (float2(1.0, 0.0)), float2(0.0), l4)));
	l7 = T0.sample(__texelSampler, floor(clamp((l2) + (float2(0.0, 1.0)), float2(0.0), l4)));
	l8 = T0.sample(__texelSampler, floor(clamp((l2) + (float2(1.0)), float2(0.0), l4)));
	return mix(mix(l5, l6, (l3).x), mix(l7, l8, (l3).x), (l3).y);
}

float4 F20(constant Uniforms& uniforms, texture2d<float> T0, texture2d<float> T1, texture2d<float> T2, texture2d<float> T3, float4 l0, float2 l1) {
	float l2 = float(0);
	uint l5 = 0u;
	l2 = 0.0;
	for (int l3 = 0; l3 < uniforms.U8; l3++) {
		l2 = (l2) + ((((uniforms.U9)[l3]).z) / ((1.0) + (distance(l1, ((uniforms.U9)[l3]).xy))));
	}
	for (uint l4 = 0u; l4 < uniforms.U7; l4 += 2u) {
		l2 = (l2) + (1.0000000000e-01);
	}
	l5 = F0(uniforms, T0, T1, T2, T3, (((uint3(static_cast<uint>(static_cast<int>((l0).x)))).z) * (2654435769u)) ^ (F0(uniforms, T0, T1, T2, T3, (static_cast<uint>(static_cast<int>((l0).y))) ^ (uniforms.U7))));
	return (((F10(uniforms, T0, T1, T2, T3, (l1) * (F2(uniforms, T0, T1, T2, T3)))) * (static_cast<float>((l5) % (255u)))) / (255.0)) * (l2);
}

float4 shaderFragment(constant Uniforms& uniforms, texture2d<float> T0, texture2d<float> T1, texture2d<float> T2, texture2d<float> T3, float4 position, float2 v0) {
	return F20(uniforms, T0, T1, T2, T3, position, v0);
}
