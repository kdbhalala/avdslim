class Avdslim < Formula
  desc "Cut Android emulator (AVD) host RAM from ~8.5 GB to ~2.5 GB on Apple Silicon & Linux"
  homepage "https://github.com/kdbhalala/avdslim"
  version "1.0.15"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/kdbhalala/avdslim/releases/download/v1.0.15/avdslim_v1.0.15_darwin_arm64.tar.gz"
      sha256 "82d80c65216a4f785965ad749cedfef6bf1e05be427274bbe8395849da796fc1"
    else
      url "https://github.com/kdbhalala/avdslim/releases/download/v1.0.15/avdslim_v1.0.15_darwin_amd64.tar.gz"
      sha256 "ca479652d550ea91391ba673b21ac0101b8ed3811f86ebf581e370198d8e9950"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/kdbhalala/avdslim/releases/download/v1.0.15/avdslim_v1.0.15_linux_arm64.tar.gz"
      sha256 "aaa4689b0089bacd34ffd2322031981eaf256c30db698d6969ee2371665897b6"
    else
      url "https://github.com/kdbhalala/avdslim/releases/download/v1.0.15/avdslim_v1.0.15_linux_amd64.tar.gz"
      sha256 "aa1396f835a42d20b0787e078926d0f7a75814558e3aae0212caad72053b3ded"
    end
  end

  def install
    bin.install "avdslim"
  end

  test do
    assert_match "avdslim", shell_output("#{bin}/avdslim version")
  end
end
