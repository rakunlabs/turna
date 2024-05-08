import{_ as s,o as n,c as a,S as p}from"./chunks/framework.662a2917.js";const A=JSON.parse('{"title":"View","description":"","frontmatter":{},"headers":[],"relativePath":"examples/view.md","filePath":"examples/view.md"}'),l={name:"examples/view.md"},o=p(`<h1 id="view" tabindex="-1">View <a class="header-anchor" href="#view" aria-label="Permalink to &quot;View&quot;">​</a></h1><p>Swagger pages in one place.</p><div class="language-yaml"><button title="Copy Code" class="copy"></button><span class="lang">yaml</span><pre class="shiki material-theme-palenight"><code><span class="line"><span style="color:#F07178;">server</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">  </span><span style="color:#F07178;">entrypoints</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">    </span><span style="color:#F07178;">web</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">      </span><span style="color:#F07178;">address</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#89DDFF;">&quot;</span><span style="color:#C3E88D;">:8082</span><span style="color:#89DDFF;">&quot;</span></span>
<span class="line"><span style="color:#A6ACCD;">  </span><span style="color:#F07178;">http</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">    </span><span style="color:#F07178;">middlewares</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">      </span><span style="color:#F07178;">info</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">        </span><span style="color:#F07178;">hello</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">          </span><span style="color:#F07178;">message</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#89DDFF;font-style:italic;">|</span></span>
<span class="line"><span style="color:#C3E88D;">            swagger_settings:</span></span>
<span class="line"><span style="color:#C3E88D;">              base_path_prefix: /api</span></span>
<span class="line"><span style="color:#C3E88D;">              disable_authorize_button: true</span></span>
<span class="line"><span style="color:#C3E88D;">              schemes: [&quot;HTTPS&quot;]</span></span>
<span class="line"><span style="color:#C3E88D;">            swagger:</span></span>
<span class="line"><span style="color:#C3E88D;">              - name: test1</span></span>
<span class="line"><span style="color:#C3E88D;">                link: https://petstore.swagger.io/v2/swagger.json</span></span>
<span class="line"><span style="color:#C3E88D;">                base_path_prefix: /api</span></span>
<span class="line"><span style="color:#C3E88D;">                disable_authorize_button: true</span></span>
<span class="line"><span style="color:#C3E88D;">              - name: test2</span></span>
<span class="line"><span style="color:#C3E88D;">                link: https://petstore.swagger.io/v2/swagger.json</span></span>
<span class="line"><span style="color:#C3E88D;">                disable_authorize_button: false</span></span>
<span class="line"><span style="color:#C3E88D;">              - name: test3</span></span>
<span class="line"><span style="color:#C3E88D;">                link: https://petstore.swagger.io/v2/swagger.json</span></span>
<span class="line"><span style="color:#A6ACCD;">      </span><span style="color:#F07178;">view</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">        </span><span style="color:#F07178;">view</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">          </span><span style="color:#F07178;">prefix_path</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">/view/</span></span>
<span class="line"><span style="color:#89DDFF;">          </span><span style="color:#676E95;font-style:italic;"># info_url: http://localhost:8082/info</span></span>
<span class="line"><span style="color:#A6ACCD;">          </span><span style="color:#F07178;">info_url_type</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">YAML</span></span>
<span class="line"><span style="color:#A6ACCD;">          </span><span style="color:#F07178;">info</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">            </span><span style="color:#F07178;">swagger_settings</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">              </span><span style="color:#F07178;">base_path_prefix</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">/api</span></span>
<span class="line"><span style="color:#A6ACCD;">              </span><span style="color:#F07178;">disable_authorize_button</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#FF9CAC;">true</span></span>
<span class="line"><span style="color:#A6ACCD;">              </span><span style="color:#F07178;">schemes</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#89DDFF;">[</span><span style="color:#89DDFF;">&quot;</span><span style="color:#C3E88D;">HTTPS</span><span style="color:#89DDFF;">&quot;</span><span style="color:#89DDFF;">]</span></span>
<span class="line"><span style="color:#A6ACCD;">            </span><span style="color:#F07178;">swagger</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">              </span><span style="color:#89DDFF;">-</span><span style="color:#A6ACCD;"> </span><span style="color:#F07178;">name</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">test1</span></span>
<span class="line"><span style="color:#A6ACCD;">                </span><span style="color:#F07178;">link</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">https://petstore.swagger.io/v2/swagger.json</span></span>
<span class="line"><span style="color:#A6ACCD;">                </span><span style="color:#F07178;">base_path_prefix</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">/api</span></span>
<span class="line"><span style="color:#A6ACCD;">                </span><span style="color:#F07178;">disable_authorize_button</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#FF9CAC;">true</span></span>
<span class="line"><span style="color:#A6ACCD;">              </span><span style="color:#89DDFF;">-</span><span style="color:#A6ACCD;"> </span><span style="color:#F07178;">name</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">test2</span></span>
<span class="line"><span style="color:#A6ACCD;">                </span><span style="color:#F07178;">link</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">https://petstore.swagger.io/v2/swagger.json</span></span>
<span class="line"><span style="color:#A6ACCD;">                </span><span style="color:#F07178;">disable_authorize_button</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#FF9CAC;">false</span></span>
<span class="line"><span style="color:#A6ACCD;">              </span><span style="color:#89DDFF;">-</span><span style="color:#A6ACCD;"> </span><span style="color:#F07178;">name</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">test3</span></span>
<span class="line"><span style="color:#A6ACCD;">                </span><span style="color:#F07178;">link</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">https://petstore.swagger.io/v2/swagger.json</span></span>
<span class="line"><span style="color:#A6ACCD;">    </span><span style="color:#F07178;">routers</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">      </span><span style="color:#F07178;">view</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">        </span><span style="color:#F07178;">path</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">/view/*</span></span>
<span class="line"><span style="color:#A6ACCD;">        </span><span style="color:#F07178;">middlewares</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">          </span><span style="color:#89DDFF;">-</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">view</span></span>
<span class="line"><span style="color:#A6ACCD;">      </span><span style="color:#F07178;">info</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">        </span><span style="color:#F07178;">path</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">/info</span></span>
<span class="line"><span style="color:#A6ACCD;">        </span><span style="color:#F07178;">middlewares</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">          </span><span style="color:#89DDFF;">-</span><span style="color:#A6ACCD;"> </span><span style="color:#C3E88D;">info</span></span></code></pre></div>`,3),e=[o];function t(c,r,D,y,C,i){return n(),a("div",null,e)}const _=s(l,[["render",t]]);export{A as __pageData,_ as default};
