import fallbackIcons from './fallbackLobeIcons'

// Keep the picker focused on OpenAI-compatible upstreams. Loading the entire
// icon catalog made the WebUI bundle needlessly large and also pulled in icons
// for removed platform integrations.
import alibaba from '../../node_modules/@lobehub/icons-static-svg/icons/alibaba.svg?raw'
import deepseek from '../../node_modules/@lobehub/icons-static-svg/icons/deepseek.svg?raw'
import kimi from '../../node_modules/@lobehub/icons-static-svg/icons/kimi.svg?raw'
import moonshot from '../../node_modules/@lobehub/icons-static-svg/icons/moonshot.svg?raw'
import openai from '../../node_modules/@lobehub/icons-static-svg/icons/openai.svg?raw'
import openrouter from '../../node_modules/@lobehub/icons-static-svg/icons/openrouter.svg?raw'
import qwen from '../../node_modules/@lobehub/icons-static-svg/icons/qwen.svg?raw'
import siliconcloud from '../../node_modules/@lobehub/icons-static-svg/icons/siliconcloud.svg?raw'
import together from '../../node_modules/@lobehub/icons-static-svg/icons/together.svg?raw'
import zhipu from '../../node_modules/@lobehub/icons-static-svg/icons/zhipu.svg?raw'

const lobeIconMap: Record<string, string> = {
  ...fallbackIcons,
  alibaba,
  deepseek,
  kimi,
  moonshot,
  openai,
  openrouter,
  qwen,
  siliconcloud,
  together,
  zhipu,
}

export default lobeIconMap
