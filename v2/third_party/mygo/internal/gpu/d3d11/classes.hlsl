// The shaders of the classes of batches (gpu.Class), after shader.hlsl, whose
// functions they share, with CLASSES defined, a bit (1 << gpu.Class) for each
// class they draw, and PLAIN, FILL, SHADOW, GLYPH, SUBPIXEL and IMAGE, each 1 if
// they draw it, as FXC's preprocessor cannot test the bits. A class's instances
// are not transformed, so these leave out the general shader's coordinates, and
// pass on only what their paths read; flow control skips what a fragment needs
// not, and only subpixel glyphs write a second color for dual-source blending:
// the others blend by their alpha, which is that color for them. They draw the
// pixels the general shader does.
//
// `go generate` compiles them to DXBC (shaders.go) on Windows.

struct ClassOut {
	float4 pos : SV_Position;
	float4 pt : PIXEL; // the position in pixels, and the texture coordinates
#if PLAIN || FILL || SHADOW || IMAGE
	nointerpolation float4 rect : RECT;
#endif
	nointerpolation float4 radii : RADII;
#if FILL || SHADOW || GLYPH || SUBPIXEL
	nointerpolation float4 inner : INNER;
#endif
	nointerpolation float4 color : COLOR0;
#if FILL || GLYPH || SUBPIXEL
	nointerpolation float4 color2 : COLOR1;
	nointerpolation float4 grad : GRAD;
#endif
#if FILL
	nointerpolation float4 border : COLOR2;
#endif
#if FILL || SHADOW || IMAGE
	nointerpolation float4 widths : WIDTHS;
#endif
	nointerpolation float4 clip : CLIP;
	nointerpolation float4 clipRadii : CLIPR;
	// The kind, plus 8 times how the clip applies (gpu.Instance's
	// Transform1.w); the rest as gpu.Instance's Params.
	nointerpolation float4 params : PARAMS;
};

ClassOut classvs(uint vid : SV_VertexID, Inst i) {
	float2 corner = float2(vid & 1, vid >> 1);
	float kind = i.params.x;
	float e = kind < 0.5 ? 1 : kind < 1.5 ? 3 * i.params.z + 1 : 0;
	float4 r = float4(i.rect.xy - e, i.rect.zw + 2 * e);
	float2 p = r.xy + corner * r.zw;
	ClassOut o;
	o.pos = float4(p / viewport * float2(2, -2) + float2(-1, 1), 0, 1);
	o.pt.xy = p;
	o.pt.zw = i.uv.xy + (p - i.rect.xy) / i.rect.zw * (i.uv.zw - i.uv.xy);
#if PLAIN || FILL || SHADOW || IMAGE
	o.rect = i.rect;
#endif
	o.radii = i.radii;
#if FILL || SHADOW || GLYPH || SUBPIXEL
	o.inner = i.inner;
#endif
	o.color = i.color;
#if FILL || GLYPH || SUBPIXEL
	o.color2 = i.color2;
	o.grad = i.grad;
#endif
#if FILL
	o.border = i.border;
#endif
#if FILL || SHADOW || IMAGE
	o.widths = i.uv;
#endif
	o.clip = i.clip;
	o.clipRadii = i.clipRadii;
	// A square clip holding the whole quad, and what a fragment's pixel reaches
	// beyond it, covers its fragments by 1: none computes that (3).
	float mode = i.transform1.w;
	if (mode < 0.5 && !any(i.clipRadii) && all(i.clip.xy <= r.xy - 1) && all(i.clip.xy + i.clip.zw >= r.xy + r.zw + 1)) {
		mode = 3;
	}
	o.params = float4(kind + 8 * mode, i.params.yzw);
	return o;
}

struct ClassPS {
	float4 color : SV_Target0;
#if SUBPIXEL
	float4 alpha : SV_Target1;
#endif
};

// classClip returns how much of the fragment the clip covers, mode saying how
// it applies.
float classClip(ClassOut i, float mode) {
	float2 p = i.pt.xy;
	[branch] if (mode > 2.5) {
		return 1;
	} else if (mode < 0.5) {
		return rectCoverage(p, i.clip, i.clipRadii);
	} else if (mode > 1.5) {
		return rectCoverage(p, i.clip, float4(0, 0, 0, 0)) * clipTex.SampleLevel(samp, (p - i.clipRadii.xy) / i.clipRadii.zw, 0).a;
	}
	return clipTex.SampleLevel(samp, (p - i.clip.xy) / i.clip.zw, 0).a;
}

// IS tells whether an instance of class c is one a test finds, which needs no
// test when the shader draws only that class.
#define IS(c, test) (CLASSES == (1 << c) || (test))

ClassPS classps(ClassOut i) {
	float2 p = i.pt.xy;
	float mode = floor(i.params.x / 8);
	float kind = i.params.x - 8 * mode;
	float4 res = float4(0, 0, 0, 0);
	ClassPS o;
#if IMAGE
	float2 dtex = fwidth(i.pt.zw);
#endif
#if FILL
	[branch] if (kind < 0.5) { // plain ones as well, when the shader draws both
		float outer = rectCoverage(p, i.rect, i.radii);
		// params.y: 1 dashes the border, 2 paints it with the gradient (the
		// inside with plain color), 3 both.
		bool borderPaint = i.params.y > 1.5;
		float4 pc = paint(p, i.params.z, i.rect, i.color, i.color2, i.grad);
		res = (borderPaint ? premul(i.color) : pc) * outer;
		float4 bw = i.widths; // top, right, bottom, left
		[branch] if (any(bw > 0)) {
			float4 ir = float4(i.rect.xy + bw.wx, i.rect.zw - bw.yz - bw.wx);
			float innerCov = 0;
			if (ir.z > 0 && ir.w > 0) {
				innerCov = rectCoverage(p, ir, i.inner);
			}
			float bc = saturate(outer - innerCov);
			float dashed = i.params.y - (borderPaint ? 2 : 0);
			[branch] if (dashed > 0.5) {
				bc *= dash(p, i.rect, bw);
			}
			float4 b = (borderPaint ? pc * float4(i.border.rgb * i.border.a, i.border.a) : premul(i.border)) * bc;
			res = b + res * (1 - b.a);
		}
	}
#elif PLAIN
	[branch] if (IS(1, kind < 0.5)) {
		res = premul(i.color) * rectCoverage(p, i.rect, i.radii);
	}
#endif
#if SHADOW
	[branch] if (IS(3, kind > 0.5 && kind < 1.5)) {
		float sigma = i.params.z;
		[branch] if (i.params.y > 0.5) {
			// An inner shadow: inside the box, what the hole, blurred, leaves.
			float hole = 0;
			[branch] if (i.widths.z > 0 && i.widths.w > 0) {
				float corner = max(max(i.inner.x, i.inner.y), max(i.inner.z, i.inner.w));
				[branch] if (sigma > 0) {
					hole = boxShadow(p, i.widths, sigma, corner);
				} else {
					hole = rectCoverage(p, i.widths, i.inner);
				}
			}
			res = premul(i.color) * (rectCoverage(p, i.rect, i.radii) * (1 - hole));
		} else {
			// Outside the box casting it, where the blur is worth computing.
			float outside = 1;
			[branch] if (i.widths.z > 0 && i.widths.w > 0) {
				outside = 1 - rectCoverage(p, i.widths, i.inner);
			}
			float s = 0;
			[branch] if (outside > 0) {
				float corner = max(max(i.radii.x, i.radii.y), max(i.radii.z, i.radii.w));
				[branch] if (sigma > 0) {
					s = boxShadow(p, i.rect, sigma, corner);
				} else {
					s = rectCoverage(p, i.rect, i.radii);
				}
			}
			res = premul(i.color) * (s * outside);
		}
	}
#endif
#if GLYPH
	[branch] if (IS(4, kind > 1.5 && kind < 2.5)) {
		float4 c = premul(i.color);
		[branch] if (i.params.z > 0.5) {
			c = paint(p, i.params.z, float4(0, 0, 0, 0), i.color, i.color2, i.grad);
		}
		res = c * textCoverage(maskTex.SampleLevel(samp, i.pt.zw, 0).r, unpremul(c), i.inner.x, i.inner.y, i.radii);
	}
#endif
#if IMAGE
	[branch] if (kind > 2.5 && kind < 3.5) {
		res = colorTex.SampleLevel(samp, i.pt.zw, 0) * i.color.a;
	} else if (kind > 3.5 && kind < 4.5) {
		res = sampleImage(imageTex, i.pt.zw, i.widths, i.params.z, dtex);
		if (i.params.y > 0.5) {
			res.rgb = dot(res.rgb, float3(0.2126, 0.7152, 0.0722));
		}
		res = tint(res, i.color) * rectCoverage(p, i.rect, i.radii);
	}
#endif
#if SUBPIXEL
	[branch] if (IS(5, kind > 4.5)) {
		float4 c = premul(i.color);
		[branch] if (i.params.z > 0.5) {
			c = paint(p, i.params.z, float4(0, 0, 0, 0), i.color, i.color2, i.grad);
		}
		float3 straight = unpremul(c);
		float3 a = subpixelCoverage(colorTex.SampleLevel(samp, i.pt.zw, 0).rgb, straight, i.inner.x, i.inner.y, i.radii);
		float3 w = a * c.a * classClip(i, mode) * i.params.w;
		float wa = (w.r + w.g + w.b) / 3;
		o.color = float4(straight * w, wa);
		o.alpha = float4(w, wa);
		return o;
	}
#endif
	o.color = res * (classClip(i, mode) * i.params.w);
#if SUBPIXEL
	o.alpha = o.color.aaaa;
#endif
	return o;
}
