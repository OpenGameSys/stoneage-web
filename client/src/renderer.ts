// WebGL2 精灵渲染：8 位页（R8UI 下标 + 调色板纹理查色）与 RGBA 页，连续使用同一纹理的图合成一次绘制。
// 加载约束见 h5/CLAUDE.md"资源格式规格" 10.5：必须关闭色彩空间转换与预乘。

export interface Texture { tex: WebGLTexture; indexed: boolean; w: number; h: number; }
export interface Quad { tex: Texture; x: number; y: number; u: number; v: number; w: number; h: number; }

const VS = `#version 300 es
in vec2 aPos; in vec2 aUv; out vec2 vUv; uniform vec2 uView;
void main(){ vUv = aUv; gl_Position = vec4(aPos / uView * vec2(2.,-2.) + vec2(-1.,1.), 0., 1.); }`;
const FS_I8 = `#version 300 es
precision highp float; in vec2 vUv; out vec4 o;
uniform highp usampler2D uPage; uniform sampler2D uPal; uniform int uRowA, uRowB; uniform float uMix;
void main(){
  uint idx = texelFetch(uPage, ivec2(vUv), 0).r;
  if (idx == 0u) discard;
  o = mix(texelFetch(uPal, ivec2(int(idx), uRowA), 0), texelFetch(uPal, ivec2(int(idx), uRowB), 0), uMix);
}`;
const FS_RGBA = `#version 300 es
precision highp float; in vec2 vUv; out vec4 o; uniform sampler2D uPage;
void main(){ vec4 c = texelFetch(uPage, ivec2(vUv), 0); if (c.a == 0.) discard; o = c; }`;

const FLOATS_PER_QUAD = 24; // 两个三角形 × 3 顶点 × (x, y, u, v)
const MAX_QUADS = 4096;

export class Renderer {
  readonly gl: WebGL2RenderingContext;
  private progI8: WebGLProgram;
  private progRgba: WebGLProgram;
  private vbo: WebGLBuffer;
  private verts = new Float32Array(MAX_QUADS * FLOATS_PER_QUAD);
  private palette: Texture | null = null;
  paletteA = 0;
  paletteB = 0;
  paletteMix = 0;
  drawCalls = 0;

  constructor(canvas: HTMLCanvasElement) {
    const gl = canvas.getContext("webgl2", { premultipliedAlpha: false, antialias: false });
    if (!gl) throw new Error("浏览器不支持 WebGL2");
    this.gl = gl;
    this.progI8 = this.program(FS_I8);
    this.progRgba = this.program(FS_RGBA);
    this.vbo = gl.createBuffer()!;
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
    gl.pixelStorei(gl.UNPACK_COLORSPACE_CONVERSION_WEBGL, gl.NONE);
    gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, false);
  }

  private program(fs: string): WebGLProgram {
    const gl = this.gl, p = gl.createProgram()!;
    for (const [type, src] of [[gl.VERTEX_SHADER, VS], [gl.FRAGMENT_SHADER, fs]] as [number, string][]) {
      const s = gl.createShader(type)!;
      gl.shaderSource(s, src);
      gl.compileShader(s);
      if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(s) ?? "shader");
      gl.attachShader(p, s);
    }
    gl.linkProgram(p);
    return p;
  }

  async loadTexture(url: string, indexed: boolean): Promise<Texture> {
    const res = await fetch(url);
    if (!res.ok) throw new Error(`${url}: ${res.status}`);
    const bmp = await createImageBitmap(await res.blob(), { colorSpaceConversion: "none", premultiplyAlpha: "none" });
    const gl = this.gl, tex = gl.createTexture()!;
    gl.bindTexture(gl.TEXTURE_2D, tex);
    if (indexed) gl.texImage2D(gl.TEXTURE_2D, 0, gl.R8UI, gl.RED_INTEGER, gl.UNSIGNED_BYTE, bmp);
    else gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, gl.RGBA, gl.UNSIGNED_BYTE, bmp);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    const t = { tex, indexed, w: bmp.width, h: bmp.height };
    bmp.close();
    return t;
  }

  setPalette(tex: Texture) { this.palette = tex; }

  /** 按顺序绘制；相邻且纹理相同的图合并成一次绘制。 */
  draw(quads: Quad[], clear: [number, number, number]) {
    const gl = this.gl, W = gl.canvas.width, H = gl.canvas.height;
    gl.viewport(0, 0, W, H);
    gl.clearColor(clear[0], clear[1], clear[2], 1);
    gl.clear(gl.COLOR_BUFFER_BIT);
    this.drawCalls = 0;
    let start = 0;
    for (let i = 1; i <= quads.length; i++) {
      if (i === quads.length || quads[i].tex !== quads[start].tex || i - start >= MAX_QUADS) {
        this.flush(quads, start, i, W, H);
        start = i;
      }
    }
  }

  private flush(quads: Quad[], from: number, to: number, W: number, H: number) {
    if (from >= to) return;
    const gl = this.gl, tex = quads[from].tex, v = this.verts;
    let o = 0;
    for (let i = from; i < to; i++) {
      const q = quads[i], x0 = q.x, y0 = q.y, x1 = q.x + q.w, y1 = q.y + q.h, u0 = q.u, v0 = q.v, u1 = q.u + q.w, v1 = q.v + q.h;
      v.set([x0, y0, u0, v0, x1, y0, u1, v0, x0, y1, u0, v1, x1, y0, u1, v0, x1, y1, u1, v1, x0, y1, u0, v1], o);
      o += FLOATS_PER_QUAD;
    }
    const p = tex.indexed ? this.progI8 : this.progRgba;
    gl.useProgram(p);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.vbo);
    gl.bufferData(gl.ARRAY_BUFFER, v.subarray(0, o), gl.STREAM_DRAW);
    const aPos = gl.getAttribLocation(p, "aPos"), aUv = gl.getAttribLocation(p, "aUv");
    gl.enableVertexAttribArray(aPos);
    gl.vertexAttribPointer(aPos, 2, gl.FLOAT, false, 16, 0);
    gl.enableVertexAttribArray(aUv);
    gl.vertexAttribPointer(aUv, 2, gl.FLOAT, false, 16, 8);
    gl.uniform2f(gl.getUniformLocation(p, "uView"), W, H);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, tex.tex);
    gl.uniform1i(gl.getUniformLocation(p, "uPage"), 0);
    if (tex.indexed && this.palette) {
      gl.activeTexture(gl.TEXTURE1);
      gl.bindTexture(gl.TEXTURE_2D, this.palette.tex);
      gl.uniform1i(gl.getUniformLocation(p, "uPal"), 1);
      gl.uniform1i(gl.getUniformLocation(p, "uRowA"), this.paletteA);
      gl.uniform1i(gl.getUniformLocation(p, "uRowB"), this.paletteB);
      gl.uniform1f(gl.getUniformLocation(p, "uMix"), this.paletteMix);
    }
    gl.drawArrays(gl.TRIANGLES, 0, (to - from) * 6);
    this.drawCalls++;
  }
}
